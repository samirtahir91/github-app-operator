/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// +build ignorecoverage

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v4"
	"net/http"
	"os"
	"sync"
	"time"

	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder" // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event" // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate" // Required for Watching
)

// GithubAppReconciler reconciles a GithubApp object
type GithubAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	lock   sync.Mutex
}

var (
	defaultRequeueAfter     = 5 * time.Minute  // Default requeue interval
	defaultTimeBeforeExpiry = 15 * time.Minute // Default time before expiry
	reconcileInterval       time.Duration      // Requeue interval (from env var)
	timeBeforeExpiry        time.Duration      // Expiry threshold (from env var)
)

const (
	gitUsername = "not-used"
)

//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;update;create;delete;watch;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;update;create;delete;watch;patch

// Reconcile function
func (r *GithubAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Acquire lock for the GitHubApp object
	r.lock.Lock()
	// Release lock
	defer r.lock.Unlock()

	l := log.FromContext(ctx)
	l.Info("Enter Reconcile")

	// Fetch the GithubApp instance
	githubApp := &githubappv1.GithubApp{}
	err := r.Get(ctx, req.NamespacedName, githubApp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("GithubApp resource not found. Ignoring since object must be deleted.")
			// Delete owned access token secret
			if err := r.deleteOwnedSecrets(ctx, githubApp); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		l.Error(err, "failed to get GithubApp")
		return ctrl.Result{}, err
	}

	/* Check if the GithubApp object is being deleted
	Remove access tokensecret if being deleted
	This should be handled by k8s garbage collection but just incase,
	we manually delete the secret.
	*/
	if !githubApp.ObjectMeta.DeletionTimestamp.IsZero() {
		l.Info("GithubApp is being deleted. Deleting managed objects.")
		// Delete owned access token secret
		if err := r.deleteOwnedSecrets(ctx, githubApp); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Call the function to check if access token required
	// Will either create the access token secret or update it
	if err := r.checkExpiryAndUpdateAccessToken(ctx, githubApp); err != nil {
		l.Error(err, "failed to check expiry and update access token")
		// Update status field 'Error' with the error message
		if updateErr := r.updateStatusWithError(ctx, githubApp, err.Error()); updateErr != nil {
			l.Error(updateErr, "failed to update status field 'Error'")
		}
		return ctrl.Result{}, err
	}

	// Call the function to check expiry and renew the access token if required
	// Always requeue the githubApp for reconcile as per `reconcileInterval`
	requeueResult := r.checkExpiryAndRequeue(ctx, githubApp)

	// Clear the error field if no errors
	if githubApp.Status.Error != "" {
		githubApp.Status.Error = ""
		if err := r.Status().Update(ctx, githubApp); err != nil {
			l.Error(err, "failed to clear status field 'Error' for GithubApp")
			return ctrl.Result{}, err
		}
	}

	// Log and return
	l.Info("End Reconcile")
	fmt.Println()
	return requeueResult, nil
}

// Function to delete the access token secret owned by the GithubApp
func (r *GithubAppReconciler) deleteOwnedSecrets(ctx context.Context, githubApp *githubappv1.GithubApp) error {
	secrets := &corev1.SecretList{}
	err := r.List(ctx, secrets, client.InNamespace(githubApp.Namespace))
	if err != nil {
		return err
	}

	for _, secret := range secrets.Items {
		for _, ownerRef := range secret.OwnerReferences {
			if ownerRef.Kind == "GithubApp" && ownerRef.Name == githubApp.Name {
				if err := r.Delete(ctx, &secret); err != nil {
					return err
				}
				break
			}
		}
	}

	return nil
}

// Function to update the status field 'Error' of a GithubApp with an error message
func (r *GithubAppReconciler) updateStatusWithError(ctx context.Context, githubApp *githubappv1.GithubApp, errMsg string) error {
	// Update the error message in the status field
	githubApp.Status.Error = errMsg
	if err := r.Status().Update(ctx, githubApp); err != nil {
		return fmt.Errorf("failed to update status field 'Error' for GithubApp: %v", err)
	}

	return nil
}

// Function to check expiry and update access token
func (r *GithubAppReconciler) checkExpiryAndUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp) error {

	l := log.FromContext(ctx)

	// Get the expiresAt status field
	expiresAt := githubApp.Status.ExpiresAt.Time

	// If expiresAt status field is not present or expiry time has already passed, generate or renew access token
	if expiresAt.IsZero() || expiresAt.Before(time.Now()) {
		return r.generateOrUpdateAccessToken(ctx, githubApp)
	}

	// Check if the access token secret exists if not reconcile immediately
	accessTokenSecretKey := client.ObjectKey{
		Namespace: githubApp.Namespace,
		Name:      fmt.Sprintf("github-app-access-token-%d", githubApp.Spec.AppId),
	}
	accessTokenSecret := &corev1.Secret{}
	if err := r.Get(ctx, accessTokenSecretKey, accessTokenSecret); err != nil {
		if apierrors.IsNotFound(err) {
			// Secret doesn't exist, reconcile straight away
			return r.generateOrUpdateAccessToken(ctx, githubApp)
		}
		// Error other than NotFound, return error
		return err
	}
	// Check if there are additional keys in the existing secret's data besides accessToken
	for key := range accessTokenSecret.Data {
		if key != "token" && key != "username" {
			l.Info("Removing invalid key in access token secret", "Key", key)
			return r.generateOrUpdateAccessToken(ctx, githubApp)
		}
	}

	// Check if the accessToken field exists and is not empty
	accessToken := string(accessTokenSecret.Data["token"])
	username := string(accessTokenSecret.Data["username"])

	// Check if the access token is a valid github token via gh api auth
	if !isAccessTokenValid(ctx, username, accessToken) {
		// If accessToken is invalid, generate or update access token
		return r.generateOrUpdateAccessToken(ctx, githubApp)
	}

	// Access token exists, calculate the duration until expiry
	durationUntilExpiry := time.Until(expiresAt)

	// If the expiry threshold met, generate or renew access token
	if durationUntilExpiry <= timeBeforeExpiry {
		l.Info(
			"Expiry threshold reached - renewing",
		)
		err := r.generateOrUpdateAccessToken(ctx, githubApp)
		return err
	}

	return nil
}

// Function to check if the access token is valid by making a request to GitHub API
func isAccessTokenValid(ctx context.Context, username string, accessToken string) bool {
	l := log.FromContext(ctx)

	// If username has been modified, renew the secret
	if username != gitUsername {
		l.Info(
			"Username key is invalid, will renew",
		)
		return false
	}

	// GitHub API endpoint for rate limit information
	url := "https://api.github.com/rate_limit"

	// Create a new HTTP client
	client := &http.Client{}

	// Create a new request
	ghReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		l.Error(err, "error creating request to GitHub API for rate limit")
		return false
	}

	// Add the access token to the request header
	ghReq.Header.Set("Authorization", "token "+accessToken)

	// Send the request
	resp, err := client.Do(ghReq)
	if err != nil {
		l.Error(err, "Error sending request to GitHub API for rate limit")
		return false
	}

	// Check if the response status code is 200 (OK)
	if resp.StatusCode != http.StatusOK {
		l.Info(
			"Access token is invalid, will renew",
			"API Response code", resp.Status,
		)
		return false
	}

	// Decode the response body into a map
	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		l.Error(err, "Error decoding response body for rate limit")
		return false
	}

	// Extract rate limit information from the map
	resources := result["resources"].(map[string]interface{})
	core := resources["core"].(map[string]interface{})
	remaining := int(core["remaining"].(float64))

	// Check if remaining rate limit is greater than 0
	if remaining <= 0 {
		l.Info("Rate limit exceeded for access token")
		return false
	}

	// Close the response body to prevent resource leaks
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Handle error if closing the response body fails
			l.Error(err, "error closing response body:")
		}
	}()

	// Rate limit is valid
	l.Info("Rate limit is valid", "Remaining requests:", remaining)
	return true
}

// Function to check expiry and requeue
func (r *GithubAppReconciler) checkExpiryAndRequeue(ctx context.Context, githubApp *githubappv1.GithubApp) ctrl.Result {
	l := log.FromContext(ctx)

	// Get the expiresAt status field
	expiresAt := githubApp.Status.ExpiresAt.Time

	// Log the next expiry time
	l.Info("Next expiry time:", "expiresAt", expiresAt)

	// Return result with no error and request reconciliation after x minutes
	l.Info("Expiry threshold:", "Time", timeBeforeExpiry)
	l.Info("Requeue after:", "Time", reconcileInterval)
	return ctrl.Result{RequeueAfter: reconcileInterval}
}

// Function to generate or update access token
func (r *GithubAppReconciler) generateOrUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp) error {
	l := log.FromContext(ctx)

	// Get the private key from the Secret
	secretName := githubApp.Spec.PrivateKeySecret
	secretNamespace := githubApp.Namespace
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret)
	if err != nil {
		l.Error(err, "failed to get Secret")
		return err
	}

	privateKey, ok := secret.Data["privateKey"]
	if !ok {
		l.Error(err, "privateKey not found in Secret")
		return fmt.Errorf("privateKey not found in Secret")
	}

	// Generate or renew access token
	accessToken, expiresAt, err := generateAccessToken(
		ctx,
		githubApp.Spec.AppId,
		githubApp.Spec.InstallId,
		privateKey,
	)
	if err != nil {
		return fmt.Errorf("failed to generate access token: %v", err)

	}

	// Create a new Secret with the access token
	accessTokenSecret := fmt.Sprintf("github-app-access-token-%d", githubApp.Spec.AppId)
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accessTokenSecret,
			Namespace: githubApp.Namespace,
		},
		StringData: map[string]string{
			"token":    accessToken,
			"username": gitUsername, // username is ignored in github auth but required
		},
	}
	accessTokenSecretKey := client.ObjectKey{
		Namespace: githubApp.Namespace,
		Name:      accessTokenSecret,
	}

	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, newSecret, r.Scheme); err != nil {
		l.Error(err, "failed to set owner reference for access token secret")
		return err
	}

	// Attempt to retrieve the existing Secret
	existingSecret := &corev1.Secret{}
	if err := r.Get(ctx, accessTokenSecretKey, existingSecret); err != nil {
		if apierrors.IsNotFound(err) {
			// Secret doesn't exist, create a new one
			if err := r.Create(ctx, newSecret); err != nil {
				l.Error(err, "failed to create Secret for access token")
				return err
			}
			l.Info(
				"Secret created for access token",
				"Secret", accessTokenSecret,
			)
			// Update the status with the new expiresAt time
			if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 10); err != nil {
				return fmt.Errorf("failed after creating secret: %v", err)
			}
			// Restart the pods is required
			if err := r.rolloutDeployment(ctx, githubApp); err != nil {
				return fmt.Errorf("failed to rollout deployment after after creating secret: %v", err)
			}
			return nil
		}
		l.Error(
			err,
			"failed to get access token secret",
			"Namespace", githubApp.Namespace,
			"Secret", accessTokenSecret,
		)
		return fmt.Errorf("failed to get access token secret: %v", err)
	}

	// Secret exists, update its data
	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, existingSecret, r.Scheme); err != nil {
		l.Error(err, "failed to set owner reference for access token secret")
		return err
	}
	// Clear existing data and set new access token data
	for k := range existingSecret.Data {
		delete(existingSecret.Data, k)
	}
	existingSecret.StringData = map[string]string{
		"token":    accessToken,
		"username": gitUsername,
	}
	if err := r.Update(ctx, existingSecret); err != nil {
		l.Error(err, "failed to update existing Secret")
		return err
	}

	// Update the status with the new expiresAt time
	if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 10); err != nil {
		return fmt.Errorf("failed after updating secret: %v", err)
	}
	// Restart the pods is required
	if err := r.rolloutDeployment(ctx, githubApp); err != nil {
		return fmt.Errorf("failed to rollout deployment after updating secret: %v", err)
	}

	l.Info("Access token updated in the existing Secret successfully")
	return nil
}

// Function to update GithubApp status field with retry up to 10 attempts
func updateGithubAppStatusWithRetry(ctx context.Context, r *GithubAppReconciler, githubApp *githubappv1.GithubApp, expiresAt metav1.Time, maxAttempts int) error {
	attempts := 0
	for {
		attempts++
		githubApp.Status.ExpiresAt = expiresAt
		err := r.Status().Update(ctx, githubApp)
		if err == nil {
			return nil // Update successful
		}
		if apierrors.IsConflict(err) {
			// Conflict error, retry the update
			if attempts >= maxAttempts {
				return fmt.Errorf("maximum retry attempts reached, failed to update GitHubApp status")
			}
			// Incremental sleep between attempts
			time.Sleep(time.Duration(attempts*2) * time.Second)
			continue
		}
		// Other error, return with the error
		return fmt.Errorf("failed to update GitHubApp status: %v", err)
	}
}

// function to generate new access token for gh app
func generateAccessToken(ctx context.Context, appID int, installationID int, privateKey []byte) (string, metav1.Time, error) {

	l := log.FromContext(ctx)

	// Parse private key
	parsedKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to parse private key: %v", err)
	}

	// Generate JWT
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", appID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)), // Expiry time is 10 minutes from now
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(parsedKey)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to sign JWT: %v", err)
	}

	// Create HTTP client and perform request to get installation token
	httpClient := &http.Client{}
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Send post request for access token
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to perform HTTP request: %v", err)
	}

	// Check error in response
	if resp.StatusCode != http.StatusCreated {
		return "", metav1.Time{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse response
	var responseBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to parse response body: %v", err)
	}

	// Extract access token and expires_at from response
	accessToken := responseBody["token"].(string)
	expiresAtString := responseBody["expires_at"].(string)
	expiresAt, err := time.Parse(time.RFC3339, expiresAtString)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to parse expire time: %v", err)
	}

	// Close the response body to prevent resource leaks
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Handle error if closing the response body fails
			l.Error(err, "error closing response body:")
		}
	}()

	return accessToken, metav1.NewTime(expiresAt), nil
}

// Function to bounce pods as per `spec.rolloutDeployment.labels` in GithubApp (in the same namespace)
//go:build ignorecoverage
func (r *GithubAppReconciler) rolloutDeployment(ctx context.Context, githubApp *githubappv1.GithubApp) error {
    l := log.FromContext(ctx)

    // Check if rolloutDeployment field is defined
    if githubApp.Spec.RolloutDeployment == nil || len(githubApp.Spec.RolloutDeployment.Labels) == 0 {
        // No action needed if rolloutDeployment is not defined or no labels are specified
        return nil
    }

    // Loop through each label specified in rolloutDeployment.labels and restart pods matching each label
    for key, value := range githubApp.Spec.RolloutDeployment.Labels {
        // Create a list options with label selector
        listOptions := &client.ListOptions{
            Namespace:     githubApp.Namespace,
            LabelSelector: labels.SelectorFromSet(map[string]string{key: value}),
        }

        // List Deployments with the label selector
        deploymentList := &appsv1.DeploymentList{}
        if err := r.List(ctx, deploymentList, listOptions); err != nil {
            return fmt.Errorf("failed to list Deployments with label %s=%s: %v", key, value, err)
        }
	
        // Trigger rolling upgrade for matching deployments
        for _, deployment := range deploymentList.Items {

			// Add a timestamp label to trigger a rolling upgrade
			deployment.Spec.Template.ObjectMeta.Labels["ghApplastUpdateTime"] = time.Now().Format("20060102150405")

			// Patch the Deployment
			if err := r.Update(ctx, &deployment); err != nil {
				return fmt.Errorf(
					"failed to upgrade deployment %s/%s: %v",
					deployment.Namespace,
					deployment.Name,
					err,
				)
			}

            // Log deployment upgrade
            l.Info(
				"Deployment rolling upgrade triggered",
				"Name",
				deployment.Name,
				"Namespace",
				deployment.Namespace,
			)
        }
    }
    return nil
}

// Define a predicate function to filter create events for access token secrets
func accessTokenSecretPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore create events for access token secrets
			return false
		},
	}
}

/*
Define a predicate function to filter events for GithubApp objects
Check if the status field in ObjectOld is unset return false
Check if ExpiresAt is valid in the new GithubApp return false
Check if Error status field is cleared return false
Ignore status update event for GithubApp
*/
func githubAppPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Compare the old and new objects
			oldGithubApp := e.ObjectOld.(*githubappv1.GithubApp)
			newGithubApp := e.ObjectNew.(*githubappv1.GithubApp)

			if oldGithubApp.Status.ExpiresAt.IsZero() &&
				!newGithubApp.Status.ExpiresAt.IsZero() {
				return false
			}
			if oldGithubApp.Status.Error != "" &&
				newGithubApp.Status.Error == "" {
				return false
			}
			return true
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Get reconcile interval from environment variable or use default value
	var err error
	reconcileIntervalStr := os.Getenv("CHECK_INTERVAL")
	reconcileInterval, err = time.ParseDuration(reconcileIntervalStr)
	if err != nil {
		// Handle case where environment variable is not set or invalid
		log.Log.Error(err, "failed to set reconcileInterval, defaulting")
		reconcileInterval = defaultRequeueAfter
	}

	// Get time before expiry from environment variable or use default value
	timeBeforeExpiryStr := os.Getenv("EXPIRY_THRESHOLD")
	timeBeforeExpiry, err = time.ParseDuration(timeBeforeExpiryStr)
	if err != nil {
		// Handle case where environment variable is not set or invalid
		log.Log.Error(err, "failed to set timeBeforeExpiry, defaulting")
		timeBeforeExpiry = defaultTimeBeforeExpiry
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch GithubApps
		For(&githubappv1.GithubApp{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, githubAppPredicate())).
		// Watch access token secrets owned by GithubApps.
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, accessTokenSecretPredicate())).
		Complete(r)
}

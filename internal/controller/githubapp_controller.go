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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v4"
	"net/http"
	"os"
	"time"

	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/controller-runtime/pkg/builder"   // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/event"     // Required for Watching
	//"sigs.k8s.io/controller-runtime/pkg/handler"   // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/predicate" // Required for Watching
)

// GithubAppReconciler reconciles a GithubApp object
type GithubAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	defaultRequeueAfter     = 5 * time.Minute  // Default requeue interval
	defaultTimeBeforeExpiry = 15 * time.Minute // Default time before expiry
	reconcileInterval       time.Duration      // Requeue interval (from env var)
	timeBeforeExpiry        time.Duration      // Expiry threshold (from env var)
)

//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;update;create;delete;watch;patch

// Reconcile function
func (r *GithubAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	log.Log.Info("Enter Reconcile", "GithubApp", req.Name, "Namespace", req.Namespace)

	// Fetch the GithubApp instance
	githubApp := &githubappv1.GithubApp{}
	err := r.Get(ctx, req.NamespacedName, githubApp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Log.Info("GithubApp resource not found. Ignoring since object must be deleted.", "GithubApp", req.Name, "Namespace", req.Namespace)
			return ctrl.Result{}, nil
		}
		l.Error(err, "Failed to get GithubApp")
		return ctrl.Result{}, err
	}

	// Call the function to check if access token required
	// Will either create the access token secret or update it
	if err := r.checkExpiryAndUpdateAccessToken(ctx, githubApp, req); err != nil {
		l.Error(err, "Failed to check expiry and update access token")
		return ctrl.Result{}, err
	}

	// Call the function to check expiry and renew the access token if required
	// Always requeue the githubApp for reconcile as per `reconcileInterval`
	requeueResult, err := r.checkExpiryAndRequeue(ctx, githubApp, req)
	if err != nil {
		l.Error(err, "Failed to check expiry and requeue")
		return requeueResult, err
	}

	// Log and return
	log.Log.Info("End Reconcile", "GithubApp", req.Name, "Namespace", req.Namespace)
	fmt.Println()
	return requeueResult, nil
}

// Function to check expiry and update access token
func (r *GithubAppReconciler) checkExpiryAndUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp, req ctrl.Request) error {
	
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

	// Check if the accessToken field exists and is not empty
	var accessToken string
	accessToken = string(accessTokenSecret.Data["accessToken"])

	// Check if the access token is a valid github token via gh api auth
	if !isAccessTokenValid(ctx, accessToken, req) {
		// If accessToken is invalid, generate or update access token
		return r.generateOrUpdateAccessToken(ctx, githubApp)
	}

	// Access token exists, calculate the duration until expiry
	durationUntilExpiry := expiresAt.Sub(time.Now())

	// If the expiry threshold met, generate or renew access token
	if durationUntilExpiry <= timeBeforeExpiry {
		log.Log.Info(
			"Expiry threshold reached - renewing",
			"GithubApp", req.Name,
			"Namespace", req.Namespace,
		)
		err := r.generateOrUpdateAccessToken(ctx, githubApp)
		return err
	}

	return nil
}

// Function to check if the access token is valid by making a request to GitHub API
func isAccessTokenValid(ctx context.Context, accessToken string, req ctrl.Request) bool {
	l := log.FromContext(ctx)

	// GitHub API endpoint for rate limit information
	url := "https://api.github.com/rate_limit"

	// Create a new HTTP client
	client := &http.Client{}

	// Create a new request
	ghReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		l.Error(err, "Error creating request to GitHub API for rate limit")
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
	// close connection
	defer resp.Body.Close()

	// Check if the response status code is 200 (OK)
	if resp.StatusCode != http.StatusOK {
		log.Log.Info(
			"Access token is invalid, will renew",
			"API Response code", resp.Status,
			"GithubApp", req.Name,
			"Namespace", req.Namespace,
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
		log.Log.Info("Rate limit exceeded for access token")
		return false
	}

	// Rate limit is valid
	log.Log.Info("Rate limit is valid", "Remaining requests:", remaining, "GithubApp", req.Name, "Namespace", req.Namespace)
	return true
}

// Fucntion to check expiry and requeue
func (r *GithubAppReconciler) checkExpiryAndRequeue(ctx context.Context, githubApp *githubappv1.GithubApp, req ctrl.Request) (ctrl.Result, error) {
	// Get the expiresAt status field
	expiresAt := githubApp.Status.ExpiresAt.Time

	// Log the next expiry time
	log.Log.Info("Next expiry time:", "expiresAt", expiresAt, "GithubApp", req.Name, "Namespace", req.Namespace)

	// Return result with no error and request reconciliation after x minutes
	log.Log.Info("Expiry threshold:", "Time", timeBeforeExpiry, "GithubApp", req.Name, "Namespace", req.Namespace)
	log.Log.Info("Requeue after:", "Time", reconcileInterval, "GithubApp", req.Name, "Namespace", req.Namespace)
	return ctrl.Result{RequeueAfter: reconcileInterval}, nil
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
		l.Error(err, "Failed to get Secret")
		return err
	}

	privateKey, ok := secret.Data["privateKey"]
	if !ok {
		l.Error(err, "privateKey not found in Secret")
		return fmt.Errorf("privateKey not found in Secret")
	}

	// Generate or renew access token
	accessToken, expiresAt, err := generateAccessToken(
		githubApp.Spec.AppId,
		githubApp.Spec.InstallId,
		privateKey,
	)
	if err != nil {
		return fmt.Errorf("Failed to generate access token: %v", err)

	}

	// Create a new Secret with the access token
	accessTokenSecret := fmt.Sprintf("github-app-access-token-%d", githubApp.Spec.AppId)
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accessTokenSecret,
			Namespace: githubApp.Namespace,
		},
		StringData: map[string]string{
			"accessToken": accessToken,
		},
	}
	accessTokenSecretKey := client.ObjectKey{
		Namespace: githubApp.Namespace,
		Name:      accessTokenSecret,
	}

	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, newSecret, r.Scheme); err != nil {
		l.Error(err, "Failed to set owner reference for access token secret")
		return err
	}

	// Attempt to retrieve the existing Secret
	existingSecret := &corev1.Secret{}
	if err := r.Get(ctx, accessTokenSecretKey, existingSecret); err != nil {
		if apierrors.IsNotFound(err) {
			// Secret doesn't exist, create a new one
			if err := r.Create(ctx, newSecret); err != nil {
				l.Error(err, "Failed to create Secret for access token")
				return err
			}
			log.Log.Info(
				"Secret created for access token",
				"Namespace", githubApp.Namespace,
				"Secret", accessTokenSecret,
			)
			// Update the status with the new expiresAt time
			if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 10); err != nil {
				return fmt.Errorf("Failed after creating secret: %v", err)
			}	
			return nil
		}
		l.Error(
			err,
			"Failed to get access token secret",
			"Namespace", githubApp.Namespace,
			"Secret", accessTokenSecret,
		)
		return fmt.Errorf("Failed to get access token secret: %v", err)
	}

	// Secret exists, update its data
	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, existingSecret, r.Scheme); err != nil {
		l.Error(err, "Failed to set owner reference for access token secret")
		return err
	}
	// Clear existing data and set new access token data
	for k := range existingSecret.Data {
		delete(existingSecret.Data, k)
	}
	existingSecret.StringData = map[string]string{
		"accessToken": accessToken,
	}
	if err := r.Update(ctx, existingSecret); err != nil {
		l.Error(err, "Failed to update existing Secret")
		return err
	}

	// Update the status with the new expiresAt time
	if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 10); err != nil {
		return fmt.Errorf("Failed after updating secret: %v", err)
	}
	
	log.Log.Info("Access token updated in the existing Secret successfully")
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
				return fmt.Errorf("Maximum retry attempts reached, failed to update GitHubApp status")
			}
			// Incremental sleep between attempts
			time.Sleep(time.Duration(attempts * 2) * time.Second)
			continue
		}
		// Other error, return with the error
		return fmt.Errorf("Failed to update GitHubApp status: %v", err)
	}
}

// function to generate new access tokenf or gh app
func generateAccessToken(appID int, installationID int, privateKey []byte) (string, metav1.Time, error) {
	// Parse private key
	parsedKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to parse private key: %v", err)
	}

	// Generate JWT
	now := time.Now()
	claims := jwt.StandardClaims{
		Issuer:    fmt.Sprintf("%d", appID),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(10 * time.Minute).Unix(), // Expiry time is 10 minutes from now
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
	// Close connection
	defer resp.Body.Close()

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

	return accessToken, metav1.NewTime(expiresAt), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Get reconcile interval from environment variable or use default value
	var err error
	reconcileIntervalStr := os.Getenv("CHECK_INTERVAL")
	reconcileInterval, err = time.ParseDuration(reconcileIntervalStr)
	if err != nil {
		// Handle case where environment variable is not set or invalid
		log.Log.Error(err, "Failed to set reconcileInterval, defaulting")
		reconcileInterval = defaultRequeueAfter
	}

	// Get time before expiry from environment variable or use default value
	timeBeforeExpiryStr := os.Getenv("EXPIRY_THRESHOLD")
	timeBeforeExpiry, err = time.ParseDuration(timeBeforeExpiryStr)
	if err != nil {
		// Handle case where environment variable is not set or invalid
		log.Log.Error(err, "Failed to set timeBeforeExpiry, defaulting")
		timeBeforeExpiry = defaultTimeBeforeExpiry
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch GithubApps
		For(&githubappv1.GithubApp{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, accessTokenSecretPredicate())).
		// Watch access token secrets owned by GithubApps.
		//Owns(&corev1.Secret{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

func accessTokenSecretPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore create events for access token secrets
			log.Log.Info("GOT CREATE FOR GITHUB APP")
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Process update events for access token secrets
			log.Log.Info("GOT UPDATE FOR GITHUB APP")
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Process delete events for access token secrets
			log.Log.Info("GOT DELETE FOR GITHUB APP")
			return true
		},
	}
}
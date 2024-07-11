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
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	githubappv1 "github-app-operator/api/v1"
	vault "github.com/hashicorp/vault/api" // vault client
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kubernetes "k8s.io/client-go/kubernetes" // k8s client
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder" // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event" // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate" // Required for Watching
)

// Struct for GithubAppReconciler
type GithubAppReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	HTTPClient  *http.Client
	VaultClient *vault.Client
	K8sClient   *kubernetes.Clientset
	lock        sync.Mutex
}

// Struct for GitHub App access token response
type Response struct {
	Token     string      `json:"token"`
	ExpiresAt metav1.Time `json:"expires_at"`
}

// Struct for GitHub App rate limit
type RateLimitInfo struct {
	Resources struct {
		Core struct {
			Remaining int `json:"remaining"`
		} `json:"core"`
	} `json:"resources"`
}

// Struct to hold the GitHub API error response
type GithubErrorResponse struct {
	Message string `json:"message"`
}

var (
	defaultRequeueAfter     = 5 * time.Minute                  // Default requeue interval
	defaultTimeBeforeExpiry = 15 * time.Minute                 // Default time before expiry
	reconcileInterval       time.Duration                      // Requeue interval (from env var)
	timeBeforeExpiry        time.Duration                      // Expiry threshold (from env var)
	vaultAudience           = os.Getenv("VAULT_ROLE_AUDIENCE") // Vault audience bound to role
	vaultRole               = os.Getenv("VAULT_ROLE")          // Vault role to use
	serviceAccountName      string                             // Controller service account
	kubernetesNamespace     string                             // Controller namespace
	privateKeyCachePath     string                             // Path to store private keys
)

const (
	gitUsername = "not-used"
)

//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;update;create;delete;watch;patch
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=get;list;update;watch;patch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create;get
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=create;get

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
			l.Info("GithubApp resource not found. Deleting managed objects and cache.")
			// Delete owned access token secret
			if err := r.deleteOwnedSecrets(ctx, githubApp); err != nil {
				return ctrl.Result{}, err
			}
			// Delete private key cache
			if err := deletePrivateKeyCache(req.Namespace, req.Name); err != nil {
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
		l.Info("GithubApp is being deleted. Deleting managed objects and cache.")
		// Delete owned access token secret
		if err := r.deleteOwnedSecrets(ctx, githubApp); err != nil {
			return ctrl.Result{}, err
		}
		// Delete private key cache
		if err := deletePrivateKeyCache(req.Namespace, req.Name); err != nil {
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
		// Raise event
		r.Recorder.Event(
			githubApp,
			"Warning",
			"FailedRenewal",
			fmt.Sprintf("Error: %s", err),
		)
		return ctrl.Result{}, err
	}

	// Call the function to check expiry and renew the access token if required
	// Always requeue the githubApp for reconcile as per `reconcileInterval`
	requeueResult := checkExpiryAndRequeue(ctx, githubApp)

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

// Function to delete private key cache file for a GithubApp
func deletePrivateKeyCache(namespace string, name string) error {

	privateKeyPath := filepath.Join(privateKeyCachePath, namespace, name)
	// Remove cached private key
	err := os.Remove(privateKeyPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cached private key: %v", err)
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
		return r.createOrUpdateAccessToken(ctx, githubApp)
	}

	// Check if the access token secret exists if not reconcile immediately
	accessTokenSecretKey := client.ObjectKey{
		Namespace: githubApp.Namespace,
		Name:      githubApp.Spec.AccessTokenSecret,
	}
	accessTokenSecret := &corev1.Secret{}
	if err := r.Get(ctx, accessTokenSecretKey, accessTokenSecret); err != nil {
		if apierrors.IsNotFound(err) {
			// Secret doesn't exist, reconcile straight away
			return r.createOrUpdateAccessToken(ctx, githubApp)
		}
		// Error other than NotFound, return error
		return err
	}
	// Check if there are additional keys in the existing secret's data besides accessToken
	for key := range accessTokenSecret.Data {
		if key != "token" && key != "username" {
			l.Info("Removing invalid key in access token secret", "Key", key)
			return r.createOrUpdateAccessToken(ctx, githubApp)
		}
	}

	// Check if the accessToken field exists and is not empty
	accessToken := string(accessTokenSecret.Data["token"])
	username := string(accessTokenSecret.Data["username"])

	// Check if the access token is a valid github token via gh api auth
	if !r.isAccessTokenValid(ctx, username, accessToken) {
		// If accessToken is invalid, generate or update access token
		return r.createOrUpdateAccessToken(ctx, githubApp)
	}

	// Access token exists, calculate the duration until expiry
	durationUntilExpiry := time.Until(expiresAt)

	// If the expiry threshold met, generate or renew access token
	if durationUntilExpiry <= timeBeforeExpiry {
		l.Info(
			"Expiry threshold reached - renewing",
		)
		return r.createOrUpdateAccessToken(ctx, githubApp)
	}

	return nil
}

// Function to check if the access token is valid by making a request to GitHub API
func (r *GithubAppReconciler) isAccessTokenValid(ctx context.Context, username string, accessToken string) bool {
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

	// Create a new request
	ghReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		l.Error(err, "error creating request to GitHub API for rate limit")
		return false
	}

	// Add the access token to the request header
	ghReq.Header.Set("Authorization", "token "+accessToken)

	// Get the rate limit from GitHub API
	// Retry the request if any secondary rate limit error
	// Return an error if max retries reached
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		// Send POST request for access token
		resp, err := r.HTTPClient.Do(ghReq)

		// if error break the loop
		if err != nil {
			l.Error(err, "error sending request to GitHub API for rate limit")
			return false
		}

		// Defer closing the response body and check for errors
		defer func() {
			err := resp.Body.Close()
			if err != nil {
				l.Error(err, "error closing response body for api rate lmiit call")
			}
		}()

		// Check if the response status code is 200 (OK)
		if resp.StatusCode == http.StatusOK {

			// Decode the response body into the struct
			var result RateLimitInfo
			err = json.NewDecoder(resp.Body).Decode(&result)
			if err != nil {
				l.Error(err, "error decoding response body for rate limit")
				return false
			}

			// Get rate limit
			remaining := result.Resources.Core.Remaining

			// Check if remaining rate limit is greater than 0
			if remaining <= 0 {
				l.Info("Rate limit exceeded for access token")
				return false
			}

			// Rate limit is valid
			l.Info("Rate limit is valid", "Remaining requests:", remaining)
			return true
		}

		// If response failed due to 403 or 429 (GitHub rate limit errors)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			l.Info("Retrying GitHub API rate limit call")
			// Try use retry-after header
			retryAfter, err := strconv.Atoi(resp.Header.Get("retry-after"))
			if err != nil {
				// default to 1s if header not present
				retryAfter = 1
			}
			waitTime := time.Duration(retryAfter) * time.Second

			// Add exponentional backoff
			waitTime *= time.Duration(1 << i)

			// Add jitter
			waitTime += time.Duration(rand.Intn(500)) * time.Millisecond

			time.Sleep(waitTime)
		} else {
			// access token is invalid, renew it
			l.Info(
				"Access token is invalid, will renew",
				"API Response code", resp.Status,
			)
			return false
		}
	}
	// max retries reached return error
	l.Error(nil, "error sending request to GitHub API for rate limit")
	return false
}

// Function to check expiry and requeue
func checkExpiryAndRequeue(ctx context.Context, githubApp *githubappv1.GithubApp) ctrl.Result {
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

// Function to get private key from a k8s secret
func (r *GithubAppReconciler) getPrivateKeyFromSecret(ctx context.Context, githubApp *githubappv1.GithubApp) ([]byte, error) {
	l := log.FromContext(ctx)

	// Get the private key from the Secret
	secretName := githubApp.Spec.PrivateKeySecret
	secretNamespace := githubApp.Namespace
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret)
	if err != nil {
		l.Error(err, "failed to get Secret")
		return []byte(""), err
	}

	privateKey, ok := secret.Data["privateKey"]
	if !ok {
		l.Error(err, "privateKey not found in Secret")
		return []byte(""), fmt.Errorf("privateKey not found in Secret")
	}
	return privateKey, nil
}

// Function to get private key from a Vault secret
func (r *GithubAppReconciler) getPrivateKeyFromVault(ctx context.Context, mountPath string, secretPath string, secretKey string) ([]byte, error) {

	// Get JWT from k8s Token Request API
	token, err := r.RequestToken(ctx, vaultAudience, kubernetesNamespace, serviceAccountName)
	if err != nil {
		return []byte(""), err
	}

	// Get private key from Vault secret with short-lived JWT
	privateKey, err := r.GetSecretWithKubernetesAuth(token, vaultRole, mountPath, secretPath, secretKey)
	if err != nil {
		return []byte(""), err
	}
	return privateKey, nil
}

// Function to get private key from a GCP secret
func (r *GithubAppReconciler) getPrivateKeyFromGcp(githubApp *githubappv1.GithubApp) ([]byte, error) {

	// Get the secret name for the GCP Secret
	secretName := githubApp.Spec.GcpPrivateKeySecret

	// Get private key from GCP Secret manager secret
	privateKey, err := r.GetSecretFromSecretMgr(secretName)
	if err != nil {
		return []byte(""), err
	}
	return privateKey, nil
}

// Function to get private key from local file cache
func getPrivateKeyFromCache(namespace string, name string) ([]byte, string, error) {

	// Try to get private key from local file system
	// Stores keys in <privateKeyCachePath>/<Namespace of githubapp>/<Name of githubapp>
	privateKeyDir := filepath.Join(privateKeyCachePath, namespace)
	privateKeyPath := filepath.Join(privateKeyDir, name)

	// Create dir if does not exist
	if _, err := os.Stat(privateKeyDir); os.IsNotExist(err) {
		if err := os.MkdirAll(privateKeyDir, 0700); err != nil {
			return []byte(""), "", fmt.Errorf("failed to create private key directory: %v", err)
		}
	}
	if _, err := os.Stat(privateKeyPath); err == nil {
		// get private key if secret file exists
		privateKey, privateKeyErr := os.ReadFile(privateKeyPath)
		if privateKeyErr != nil {
			return []byte(""), "", fmt.Errorf("failed to read private key from file: %v", privateKeyErr)
		}
		return privateKey, privateKeyPath, nil
	}
	// Return privateKeyPath if private key file doesn't exist
	return []byte(""), privateKeyPath, nil
}

// Function to get private key from cache, vault or k8s secret
func (r *GithubAppReconciler) getPrivateKey(ctx context.Context, githubApp *githubappv1.GithubApp) ([]byte, string, error) {

	var privateKey []byte
	var privateKeyPath string
	var privateKeyErr error

	// Try to get private key from local file system
	privateKey, privateKeyPath, privateKeyErr = getPrivateKeyFromCache(githubApp.Namespace, githubApp.Name)
	if privateKeyErr != nil {
		return []byte(""), "", privateKeyErr
	}

	// If private key file is not cached try to get it from Vault
	// Get the private key from a vault path if defined in Githubapp spec
	// Vault auth will take precedence over using `spec.privateKeySecret`
	if githubApp.Spec.VaultPrivateKey != nil && len(privateKey) == 0 {

		if r.VaultClient.Address() == "" || vaultAudience == "" || vaultRole == "" {
			return []byte(""), "", fmt.Errorf("failed on vault auth: VAULT_ROLE, VAULT_ROLE_AUDIENCE and VAULT_ADDR are required env variables for Vault authentication")
		}

		mountPath := githubApp.Spec.VaultPrivateKey.MountPath
		secretPath := githubApp.Spec.VaultPrivateKey.SecretPath
		secretKey := githubApp.Spec.VaultPrivateKey.SecretKey
		privateKey, privateKeyErr = r.getPrivateKeyFromVault(ctx, mountPath, secretPath, secretKey)
		if privateKeyErr != nil {
			return []byte(""), "", fmt.Errorf("failed to get private key from vault: %v", privateKeyErr)
		}
		if len(privateKey) == 0 {
			return []byte(""), "", fmt.Errorf("empty private key from vault")
		}
		// Cache the private key to file
		if err := os.WriteFile(privateKeyPath, privateKey, 0600); err != nil {
			return []byte(""), "", fmt.Errorf("failed to write private key to file: %v", err)
		}
	} else if githubApp.Spec.GcpPrivateKeySecret != "" && len(privateKey) == 0 {
		// else get the private key from GCP secret `spec.googlePrivateKeySecret`
		privateKey, privateKeyErr = r.getPrivateKeyFromGcp(githubApp)
		if privateKeyErr != nil {
			return []byte(""), "", fmt.Errorf("failed to get private key from GCP secret: %v", privateKeyErr)
		}
		if len(privateKey) == 0 {
			return []byte(""), "", fmt.Errorf("empty private key from GCP")
		}
		// Cache the private key to file
		if err := os.WriteFile(privateKeyPath, privateKey, 0600); err != nil {
			return []byte(""), "", fmt.Errorf("failed to write private key to file: %v", err)
		}
	} else if githubApp.Spec.PrivateKeySecret != "" && len(privateKey) == 0 {
		// else get the private key from K8s secret `spec.privateKeySecret`
		privateKey, privateKeyErr = r.getPrivateKeyFromSecret(ctx, githubApp)
		if privateKeyErr != nil {
			return []byte(""), "", fmt.Errorf("failed to get private key from kubernetes secret: %v", privateKeyErr)
		}
		if len(privateKey) == 0 {
			return []byte(""), "", fmt.Errorf("empty private key from k8s secret")
		}
		// Cache the private key to file
		if err := os.WriteFile(privateKeyPath, privateKey, 0600); err != nil {
			return []byte(""), "", fmt.Errorf("failed to write private key to file: %v", err)
		}
	}

	return privateKey, privateKeyPath, nil
}

// Function to create access token secret
func (r *GithubAppReconciler) createAccessTokenSecret(ctx context.Context, accessTokenSecret string, accessToken string, expiresAt metav1.Time, githubApp *githubappv1.GithubApp) error {
	l := log.FromContext(ctx)

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

	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, newSecret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference for access token secret: %v", err)
	}

	// Secret doesn't exist, create a new one
	if err := r.Create(ctx, newSecret); err != nil {
		return err
	}
	l.Info(
		"Secret created for access token",
		"Secret", accessTokenSecret,
	)
	// Raise event
	r.Recorder.Event(
		githubApp,
		"Normal",
		"Created",
		fmt.Sprintf("Created access token secret %s/%s", githubApp.Namespace, accessTokenSecret),
	)
	// Update the status with the new expiresAt time
	if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 3); err != nil {
		return fmt.Errorf("failed after creating secret: %v", err)
	}
	// Rollout deployments if required
	if err := r.rolloutDeployment(ctx, githubApp); err != nil {
		// Raise event
		r.Recorder.Event(
			githubApp,
			"Warning",
			"FailedDeploymentUpgrade",
			fmt.Sprintf("Error: %s", err),
		)
		return fmt.Errorf("failed to rollout deployment after after creating secret: %v", err)
	}
	return nil
}

// Function to update access token secret
func (r *GithubAppReconciler) updateAccessTokenSecret(ctx context.Context, existingSecret *corev1.Secret, accessTokenSecret string, accessToken string, expiresAt metav1.Time, githubApp *githubappv1.GithubApp) error {
	l := log.FromContext(ctx)
	// Set owner reference to GithubApp object
	if err := controllerutil.SetControllerReference(githubApp, existingSecret, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference for access token secret: %v", err)
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
		return err
	}

	// Update the status with the new expiresAt time
	if err := updateGithubAppStatusWithRetry(ctx, r, githubApp, expiresAt, 3); err != nil {
		return fmt.Errorf("failed after updating secret: %v", err)
	}
	// Restart the pods is required
	if err := r.rolloutDeployment(ctx, githubApp); err != nil {
		// Raise event
		r.Recorder.Event(
			githubApp,
			"Warning",
			"FailedDeploymentUpgrade",
			fmt.Sprintf("Error: %s", err),
		)
		return fmt.Errorf("failed to rollout deployment after updating secret: %v", err)
	}

	l.Info("Access token updated in the existing Secret successfully")
	// Raise event
	r.Recorder.Event(
		githubApp,
		"Normal",
		"Updated",
		fmt.Sprintf("Updated access token secret %s/%s", githubApp.Namespace, accessTokenSecret),
	)
	return nil
}

// Function to get a new access token and create or update a kubernetes secret with it
func (r *GithubAppReconciler) createOrUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp) error {
	l := log.FromContext(ctx)

	// Try to get private key from local file system
	privateKey, privateKeyPath, privateKeyErr := r.getPrivateKey(ctx, githubApp)
	if privateKeyErr != nil {
		return privateKeyErr
	}

	// Generate or renew access token
	accessToken, expiresAt, err := r.generateAccessToken(
		ctx,
		githubApp.Spec.AppId,
		githubApp.Spec.InstallId,
		privateKey,
	)
	// if GitHub API request for access token fails
	if err != nil {
		// Delete private key cache
		l.Error(nil, "Access token request failed, removing cached private key", "file", privateKeyPath)
		if err := deletePrivateKeyCache(githubApp.Namespace, githubApp.Name); err != nil {
			l.Error(err, "failed to remove cached private key")
		}
		return fmt.Errorf("failed to generate access token: %v", err)
	}

	// Access token Kubernetes secret name
	accessTokenSecret := githubApp.Spec.AccessTokenSecret

	// Access token secret key
	accessTokenSecretKey := client.ObjectKey{
		Namespace: githubApp.Namespace,
		Name:      accessTokenSecret,
	}

	// Attempt to retrieve the existing Secret
	existingSecret := &corev1.Secret{}

	if err := r.Get(ctx, accessTokenSecretKey, existingSecret); err != nil {
		// Secret does not exist, create it
		if apierrors.IsNotFound(err) {
			if err := r.createAccessTokenSecret(ctx, accessTokenSecret, accessToken, expiresAt, githubApp); err != nil {
				l.Error(err, "failed to create Secret for access token")
				return err
			}
			// secret created successfully, return here
			return nil
		}
		// failed to create secret
		l.Error(
			err,
			"failed to get access token secret",
			"Namespace", githubApp.Namespace,
			"Secret", accessTokenSecret,
		)
		return fmt.Errorf("failed to get access token secret: %v", err)
	}

	// Secret exists, update it's data
	if err := r.updateAccessTokenSecret(ctx, existingSecret, accessTokenSecret, accessToken, expiresAt, githubApp); err != nil {
		l.Error(err, "failed to update Secret for access token")
		return err
	}

	return nil
}

// Function to update GithubApp status field with retry up to maxAttempts attempts
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

// Function to generate new access token for gh app
func (r *GithubAppReconciler) generateAccessToken(ctx context.Context, appID int, installationID int, privateKey []byte) (string, metav1.Time, error) {

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

	// Use HTTP client and perform request to get installation token
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	// Get the access token from GitHub API
	// Retry the request if any rate limit error
	// Return an error if max retries reached
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		// Send POST request for access token
		resp, err := r.HTTPClient.Do(req)

		// if error break the loop
		if err != nil {
			return "", metav1.Time{}, fmt.Errorf("failed to send HTTP post request to GitHub API: %v", err)
		}

		// Defer closing the response body and check for errors
		defer func() {
			err := resp.Body.Close()
			if err != nil {
				l.Error(err, "error closing response body for access token call")
			}
		}()

		// If response is successful, parse token and expiry
		if resp.StatusCode == http.StatusCreated {
			// Parse response
			var responseBody Response
			// if error in body break the loop, return error msg
			if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
				return "", metav1.Time{}, fmt.Errorf("failed to parse response body: %v", err)
			}

			// Got token and expiry
			// return and break the loop
			return responseBody.Token, responseBody.ExpiresAt, nil
		}

		// If response failed due to 403 or 429 (GitHub rate limit errors)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			l.Info("Retrying GitHub API access token call")
			// Try use retry-after header
			retryAfter, err := strconv.Atoi(resp.Header.Get("retry-after"))
			if err != nil {
				// default to 1s if header not present
				retryAfter = 1
			}
			waitTime := time.Duration(retryAfter) * time.Second

			// Add exponentional backoff
			waitTime *= time.Duration(1 << i)

			// Add jitter
			waitTime += time.Duration(rand.Intn(500)) * time.Millisecond

			time.Sleep(waitTime)
		} else {
			// If not a rate limit error/any other error
			return "", metav1.Time{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}
	// max retries reached return error
	return "", metav1.Time{}, fmt.Errorf("failed to get access token after %d retries", maxRetries)
}

// Function to upgrade deployments as per `spec.rolloutDeployment.labels` in GithubApp (in the same namespace)
func (r *GithubAppReconciler) rolloutDeployment(ctx context.Context, githubApp *githubappv1.GithubApp) error {
	l := log.FromContext(ctx)

	// Check if rolloutDeployment field is defined
	if githubApp.Spec.RolloutDeployment == nil || len(githubApp.Spec.RolloutDeployment.Labels) == 0 {
		// No action needed if rolloutDeployment is not defined or no labels are specified
		return nil
	}

	// Loop through each label specified in rolloutDeployment.labels and update deployments matching each label
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
			// Raise event
			r.Recorder.Event(
				githubApp,
				"Normal",
				"Updated",
				fmt.Sprintf("Updated deployment %s/%s", deployment.Namespace, deployment.Name),
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

// Function to get service account and namespace of controller
func getServiceAccountAndNamespace(serviceAccountPath string) (string, string, error) {

	// Get KSA mounted in pod
	serviceAccountToken, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read service account token: %v", err)
	}
	// Parse the KSA token
	parsedToken, _, err := new(jwt.Parser).ParseUnverified(string(serviceAccountToken), jwt.MapClaims{})
	if err != nil {
		return "", "", fmt.Errorf("failed to parse token: %v", err)
	}
	// Get the claims
	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("failed to parse token claims")
	}
	// Get kubernetes.io claims
	kubernetesClaims, ok := claims["kubernetes.io"].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("failed to assert kubernetes.io claim to map[string]interface{}")
	}
	// Get serviceaccount claim
	serviceAccountClaims, ok := kubernetesClaims["serviceaccount"].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("failed to assert serviceaccount claim to map[string]interface{}")
	}
	// Get the namespace
	kubernetesNamespace, ok := kubernetesClaims["namespace"].(string)
	if !ok {
		return "", "", fmt.Errorf("failed to assert namespace to string")
	}
	// Get service account name
	serviceAccountName, ok := serviceAccountClaims["name"].(string)
	if !ok {
		return "", "", fmt.Errorf("failed to assert service account name to string")
	}

	return serviceAccountName, kubernetesNamespace, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAppReconciler) SetupWithManager(mgr ctrl.Manager, privateKeyCache string, tokenPath ...string) error {

	// Set private key cache path
	privateKeyCachePath = privateKeyCache

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

	// Get service account name and namespace
	// Check if tokenPath is provided
	var serviceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if len(tokenPath) > 0 {
		serviceAccountPath = tokenPath[0]
	}

	serviceAccountName, kubernetesNamespace, err = getServiceAccountAndNamespace(serviceAccountPath)
	if err != nil {
		log.Log.Error(err, "failed to get service account and/or namespace of controller")
	} else {
		log.Log.Info("got controller service account and namespace", "service account", serviceAccountName, "namespace", kubernetesNamespace)
	}

	return ctrl.NewControllerManagedBy(mgr).
		// Watch GithubApps
		For(&githubappv1.GithubApp{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, githubAppPredicate())).
		// Watch access token secrets owned by GithubApps.
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, accessTokenSecretPredicate())).
		Complete(r)
}

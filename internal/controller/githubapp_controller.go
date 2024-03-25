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
	"net/http"
	"time"
	"github.com/golang-jwt/jwt/v4"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"	
)

// GithubAppReconciler reconciles a GithubApp object
type GithubAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/finalizers,verbs=update

// Reconcile function
func (r *GithubAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    l := log.FromContext(ctx)
    l.Info("Reconciling GithubApp")

    // Fetch the GithubApp resource
    githubApp := &githubappv1.GithubApp{}
    err := r.Get(ctx, req.NamespacedName, githubApp)
    if err != nil {
        l.Error(err, "Failed to get GithubApp")
        return ctrl.Result{}, err
    }

    // Check expiry and generate access token if needed
    result, err := r.checkExpiryAndUpdateAccessToken(ctx, githubApp)
    if err != nil {
        l.Error(err, "Failed to check expiry and update access token")
        return ctrl.Result{}, err
    }

    // Requeue after a certain duration
    return result, nil
}

// Function to check expiry and update access token
func (r *GithubAppReconciler) checkExpiryAndUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp) (ctrl.Result, error) {
    // Get the expiresAt status field
    expiresAt := githubApp.Status.ExpiresAt

    // If expiresAt status field is not present or expiry time has already passed, generate or renew access token
    if expiresAt.Before(time.Now()) {
        return r.generateOrUpdateAccessToken(ctx, githubApp)
    }

    // Calculate the duration until expiry
    durationUntilExpiry := expiresAt.Time.Sub(time.Now())

    // If the expiry is within the next 10 minutes, generate or renew access token
    if durationUntilExpiry <= 10*time.Minute {
        return r.generateOrUpdateAccessToken(ctx, githubApp)
    }

    // Log the next expiry time
    log.Log.Info("Next expiry time:", "expiresAt", expiresAt.Time)

    // Return result with no error
    return ctrl.Result{}, nil
}

// Function to generate or update access token
func (r *GithubAppReconciler) generateOrUpdateAccessToken(ctx context.Context, githubApp *githubappv1.GithubApp) (ctrl.Result, error) {
    l := log.FromContext(ctx)

    // Get the private key from the Secret
    secretName := githubApp.Spec.PrivateKeySecret
    secretNamespace := githubApp.Namespace
    secret := &corev1.Secret{}
    err := r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret)
    if err != nil {
        l.Error(err, "Failed to get Secret")
        return ctrl.Result{}, err
    }

    privateKey, ok := secret.Data["privateKey"]
    if !ok {
        l.Error(err, "privateKey not found in Secret")
        return ctrl.Result{}, fmt.Errorf("privateKey not found in Secret")
    }

    // Generate or renew access token
    accessToken, expiresAt, err := generateAccessToken(githubApp.Spec.AppId, githubApp.Spec.InstallId, privateKey)
    if err != nil {
        return ctrl.Result{}, err
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
        return ctrl.Result{}, err
    }

    // Attempt to retrieve the existing Secret
    existingSecret := &corev1.Secret{}
    if err := r.Get(ctx, accessTokenSecretKey, existingSecret); err != nil {
        if apierrors.IsNotFound(err) {
            // Secret doesn't exist, create a new one
            if err := r.Create(ctx, newSecret); err != nil {
                l.Error(err, "Failed to create Secret for access token")
                return ctrl.Result{}, err
            }
            log.Log.Info("Secret created for access token", "Namespace", githubApp.Namespace, "Secret", accessTokenSecret)
            return ctrl.Result{}, nil
        }
        l.Error(err, "Failed to get access token secret", "Namespace", githubApp.Namespace, "Secret", accessTokenSecret)
        return ctrl.Result{}, err // Return both error and ctrl.Result{}
    }

    // Secret exists, update its data
    // Set owner reference to GithubApp object
    if err := controllerutil.SetControllerReference(githubApp, existingSecret, r.Scheme); err != nil {
        l.Error(err, "Failed to set owner reference for access token secret")
        return ctrl.Result{}, err
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
        return ctrl.Result{}, err
    }

    // Update the status with the new expiresAt time
    githubApp.Status.ExpiresAt = metav1.NewTime(expiresAt)
    if err := r.Status().Update(ctx, githubApp); err != nil {
        return ctrl.Result{}, err
    }

    l.Info("Access token updated in the existing Secret successfully")
    return ctrl.Result{}, nil
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", metav1.Time{}, fmt.Errorf("failed to perform HTTP request: %v", err)
	}
	defer resp.Body.Close()

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
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubappv1.GithubApp{}).
		// Watch secrets owned by GithubApps.
		Owns(&corev1.Secret{}).
		Complete(r)
}

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
	"reflect"

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
	"sigs.k8s.io/controller-runtime/pkg/builder"   // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/event"     // Required for Watching
	"sigs.k8s.io/controller-runtime/pkg/predicate" // Required for Watching
	
)

// GithubAppReconciler reconciles a GithubApp object
type GithubAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=githubapp.samir.io,resources=githubapps/finalizers,verbs=update

// Reconcile fucntion
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

	// Get the private key from the Secret
	secretName := githubApp.Spec.PrivateKeySecret
	secretNamespace := githubApp.Namespace
	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret)
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
	accessToken, err := generateAccessToken(githubApp.Spec.AppId, githubApp.Spec.InstallId, privateKey)
	if err != nil {
		l.Error(err, "Failed to generate or renew access token")
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
	existingSecret.StringData = newSecret.StringData
	if err := r.Update(ctx, existingSecret); err != nil {
		l.Error(err, "Failed to update existing Secret")
		return ctrl.Result{}, err
	}

	l.Info("Access token updated in the existing Secret successfully")
	return ctrl.Result{}, nil
}

func generateAccessToken(appID int, installationID int, privateKey []byte) (string, error) {
	// Parse private key
	parsedKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
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
		return "", fmt.Errorf("failed to sign JWT: %v", err)
	}

	// Create HTTP client and perform request to get installation token
	httpClient := &http.Client{}
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse response
	var responseBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		return "", fmt.Errorf("failed to parse response body: %v", err)
	}

	// Extract access token from response
	accessToken, ok := responseBody["token"].(string)
	if !ok {
		return "", fmt.Errorf("failed to extract access token from response")
	}

	return accessToken, nil
}

// Define a predicate function to filter out reconciles for unchanged secrets
func (r *GithubAppReconciler) destinationNamespacePredicate(ctx context.Context) predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			logctx := log.FromContext(ctx)

			// Fetch objects
			newSecret := &corev1.Secret{}
			oldSecret := &corev1.Secret{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: e.ObjectNew.GetNamespace(), Name: e.ObjectNew.GetName()}, newSecret); err != nil {
				logctx.Error(err, "Failed to get new secret", "Namespace", e.ObjectNew.GetNamespace(), "Name", e.ObjectNew.GetName())
				return false
			}
			if err := r.Get(ctx, client.ObjectKey{Namespace: e.ObjectOld.GetNamespace(), Name: e.ObjectOld.GetName()}, oldSecret); err != nil {
				logctx.Error(err, "Failed to get old secret", "Namespace", e.ObjectOld.GetNamespace(), "Name", e.ObjectOld.GetName())
				return false
			}

			// Ignore update events where the oldSecret secret data and newSecret secret data are identical
			log.Log.Info("Old secret data", "data", oldSecret.Data)
			log.Log.Info("New secret data", "data", newSecret.Data)

			if reflect.DeepEqual(oldSecret.Data, newSecret.Data) {
				return false
			} else {
				return true
			}
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubappv1.GithubApp{}).
		// Watch secrets owned by GithubApps.
		Owns(&corev1.Secret{}, builder.WithPredicates(r.destinationNamespacePredicate(ctx))).
		Complete(r)
}

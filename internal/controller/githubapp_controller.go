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
	"encoding/base64"
	"fmt"
	"log"
	"time"

	"github.com/google/go-github/v38/github"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	
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
	_ = log.FromContext(ctx)
	_.Info("Reconciling GithubApp")

	// Fetch the GithubApp resource
	githubApp := &githubappv1.GithubApp{}
	err := r.Get(ctx, req.NamespacedName, githubApp)
	if err != nil {
		_.Error(err, "Failed to get GithubApp")
		return ctrl.Result{}, err
	}

	// Get the private key from the Secret
	secretName := githubApp.Spec.SecretName
	secretNamespace := githubApp.Namespace
	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret)
	if err != nil {
		_.Error(err, "Failed to get Secret")
		return ctrl.Result{}, err
	}

	privateKeyEncoded, ok := secret.Data["privateKey"]
	if !ok {
		_.Error(err, "privateKey not found in Secret")
		return ctrl.Result{}, fmt.Errorf("privateKey not found in Secret")
	}

	// Decode the private key
	privateKey, err := base64.StdEncoding.DecodeString(string(privateKeyEncoded))
	if err != nil {
		_.Error(err, "Failed to decode privateKey")
		return ctrl.Result{}, err
	}

	// Generate or renew access token
	accessToken, err := generateOrRenewAccessToken(githubApp.Spec.AppID, githubApp.Spec.InstallationID, privateKey)
	if err != nil {
		_.Error(err, "Failed to generate or renew access token")
		return ctrl.Result{}, err
	}

	// Create a new Secret with the access token
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "github-app-access-token",
			Namespace: githubApp.Namespace,
		},
		StringData: map[string]string{
			"accessToken": accessToken,
		},
	}
	if err := r.Create(ctx, newSecret); err != nil {
		_.Error(err, "Failed to create Secret for access token")
		return ctrl.Result{}, err
	}

	_.Info("Access token generated and stored in Secret successfully")
	return ctrl.Result{}, nil
}

func generateOrRenewAccessToken(appID, installationID string, privateKey []byte) (string, error) {
	ctx := context.Background()

	// Create a new GitHub App client
	tr := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: "", TokenType: "Bearer"},
	))

	client, err := github.NewEnterpriseClient("https://api.github.com", "https://<YOUR_HOSTNAME>/api/v3", tr)
	if err != nil {
		return "", fmt.Errorf("error creating GitHub client: %s", err)
	}

	// Create a JWT token for authentication
	token, err := github.NewJWTClient(ctx, tr, int(appID), privateKey)
	if err != nil {
		return "", fmt.Errorf("error creating JWT client: %s", err)
	}

	// Get installation token
	instToken, _, err := token.InstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", fmt.Errorf("error getting installation token: %s", err)
	}

	return instToken.GetToken(), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GithubAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&githubappv1.GithubApp{}).
		Complete(r)
}

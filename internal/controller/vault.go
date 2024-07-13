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

	"k8s.io/utils/ptr"

	auth "github.com/hashicorp/vault/api/auth/kubernetes" // vault k8s auth
	authenticationv1 "k8s.io/api/authentication/v1"       // k8s Token request
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Function to create token via K8s Token Request API
func (r *GithubAppReconciler) RequestToken(
	ctx context.Context,
	vaultAudience string,
	kubernetesNamespace string,
	serviceAccountName string,
) (string, error) {

	// Token request spec
	// TTL of 10 mins for short lived JWT for Vault auth
	treq := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{vaultAudience},
			ExpirationSeconds: ptr.To(int64(600)),
		},
	}

	// Request a JWT from Token Request API
	tokenRequest, err := r.K8sClient.CoreV1().ServiceAccounts(kubernetesNamespace).CreateToken(
		ctx,
		serviceAccountName,
		treq,
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create token request to k8s api: %v", err)
	}
	token := tokenRequest.Status.Token
	return token, nil
}

// Fetches a key-value secret (kv-2) after authenticating to Vault with a Kubernetes service account
func (r *GithubAppReconciler) GetSecretWithKubernetesAuth(
	token string,
	vaultRole string,
	mountPath string,
	secretPath string,
	secretKey string,
) ([]byte, error) {

	// Auth to Vault using k8s auth, role and short-lived JWT with defined audience
	k8sAuth, err := auth.NewKubernetesAuth(
		vaultRole,
		auth.WithServiceAccountToken(token),
	)
	if err != nil {
		return []byte(""), fmt.Errorf("failed auth to vault using k8s auth with JWT: %v", err)
	}
	authInfo, err := r.VaultClient.Auth().Login(context.Background(), k8sAuth)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to login to vault with k8s auth: %v", err)
	}
	if authInfo == nil {
		return []byte(""), fmt.Errorf("no auth info returned after login to vault")
	}

	// Get secret from vault mount path
	secret, err := r.VaultClient.KVv2(mountPath).Get(context.Background(), secretPath)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to read secret in vault: %v", err)
	}

	// Get private key data as string
	privateKeyStr, ok := secret.Data[secretKey].(string)
	if !ok {
		return []byte(""), fmt.Errorf("failed type assertion on vault secret data")
	}
	// Base64 decode the private key
	// The private key must be stored as a base64 encoded string in the vault secret
	privateKey, err := base64.StdEncoding.DecodeString(privateKeyStr)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to base64 decode the private key: %v", err)
	}

	return privateKey, nil
}

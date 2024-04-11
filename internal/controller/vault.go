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
	"github.com/golang-jwt/jwt/v4"
	"io/ioutil"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	// vault auth
	vault "github.com/hashicorp/vault/api"
	auth "github.com/hashicorp/vault/api/auth/kubernetes"
	// k8s Token request 
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Function to create token via K8s Token Request API
func RequestToken(ctx context.Context, vaultAudience string) (string, error) {
	// Auth to k8s using mounted service account
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("failed to use in cluster config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to set k8s clientset: %v", err)
	}

	// Token request spec 
	// TTL of 10 mins for short lived JWT for Vault auth
	treq := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:		   []string{vaultAudience},
			ExpirationSeconds: ptr.To(int64(600)),
		},
	}

	// Get KSA mounted in pod
	serviceAccountToken, err := ioutil.ReadFile("var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}
	// Parse the KSA token
	parsedToken, _, err := new(jwt.Parser).ParseUnverified(string(serviceAccountToken), jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("failed to parse token: %v", err)
	}
	// Get the claims
	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("failed to parse token claims")
	}
	// Get kubernetes.io claims
	kubernetesClaims, ok := claims["kubernetes.io"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("failed to assert kubernetes.io claim to map[string]interface{}")
	}
	// Get serviceaccount claim
	serviceAccountClaims, ok := kubernetesClaims["serviceaccount"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("failed to assert serviceaccount claim to map[string]interface{}")
	}
	// Get the namespace
	kubernetesNamespace, ok := kubernetesClaims["namespace"].(string)
	if !ok {
		return "", fmt.Errorf("failed to assert namespace to string")
	}
	// Get service account name
	serviceAccountName, ok := serviceAccountClaims["name"].(string)
	if !ok {
		return "", fmt.Errorf("failed to assert service account name to string")
	}

	// Request a JWT from Token Request API
	tokenRequest, tErr := clientset.CoreV1().ServiceAccounts(kubernetesNamespace).CreateToken(
		ctx,
		serviceAccountName,
		treq,
		metav1.CreateOptions{},
	)
	if tErr != nil {
		return "", fmt.Errorf("failed to create token request to k8s api: %v", err)
	}
	token := tokenRequest.Status.Token
	return token, nil
}

// Fetches a key-value secret (kv-2) after authenticating to Vault with a Kubernetes service account
func GetSecretWithKubernetesAuth(
	token string,
	vaultAddress string,
	vaultAudience string,
	mountPath string,
	secretPath string,
	secretKey string,
) ([]byte, error) {

	// Initialise vault client
	client, err := vault.NewClient(&vault.Config{
		Address: vaultAddress,
	})
	if err != nil {
		return []byte(""), fmt.Errorf("failed to initialise Vault client: %v", err)
	}

	// Auth to Vault using k8s auth, using short-lived JWT with defined audience
	k8sAuth, err := auth.NewKubernetesAuth(
		vaultAudience,
		auth.WithServiceAccountToken(token),
	)
	if err != nil {
		return []byte(""), fmt.Errorf("failed auth to vault using k8s auth with JWT: %v", err)
	}
	authInfo, err := clientAuth().Login(context.Background(), k8sAuth)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to login to vault with k8s auth: %v", err)
	}
	if authInfo == nil {
		return []byte(""), fmt.Errorf("no auth info returned after login to vault")
	}

	// Get secret from vault mount path
	secret, err := client.KVv2(mountPath).Get(context.Background(), secretPath)
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
	privateKey, err := base64.StdEncoding.Decoding(privateKeyStr)
	if err != nil {
		return []byte(""), fmt.Errorf("failed to base64 decode the private key: %v", err)
	}

	return privateKey, nil
}
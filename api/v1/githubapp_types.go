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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GithubAppSpec defines the desired state of GithubApp
type GithubAppSpec struct {
	AppId               int                    `json:"appId"`
	InstallId           int                    `json:"installId"`
	PrivateKeySecret    string                 `json:"privateKeySecret,omitempty"`
	RolloutDeployment   *RolloutDeploymentSpec `json:"rolloutDeployment,omitempty"`
	VaultPrivateKey     *VaultPrivateKeySpec   `json:"vaultPrivateKey,omitempty"`
	AccessTokenSecret   string                 `json:"accessTokenSecret"`
	GcpPrivateKeySecret string                 `json:"googlePrivateKeySecret,omitempty"`
}

// GithubAppStatus defines the observed state of GithubApp
type GithubAppStatus struct {
	// Expiry of access token
	ExpiresAt metav1.Time `json:"expiresAt,omitempty"`
	// Error field to store error messages
	Error string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GithubApp is the Schema for the githubapps API
// +kubebuilder:printcolumn:name="App ID",type=string,JSONPath=`.spec.appId`
// +kubebuilder:printcolumn:name="Access Token Secret",type=string,JSONPath=`.spec.accessTokenSecret`
// +kubebuilder:printcolumn:name="Install ID",type=string,JSONPath=`.spec.installId`
// +kubebuilder:printcolumn:name="Expires At",type=string,JSONPath=`.status.expiresAt`
// +kubebuilder:printcolumn:name="Error",type=string,JSONPath=`.status.error`
type GithubApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubAppSpec   `json:"spec,omitempty"`
	Status GithubAppStatus `json:"status,omitempty"`
}

// RolloutDeploymentSpec defines the specification for restarting pods
type RolloutDeploymentSpec struct {
	Labels map[string]string `json:"labels,omitempty"`
}

// VaultPrivateKeySpec defines the spec for retrieving the private key from Vault
type VaultPrivateKeySpec struct {
	MountPath  string `json:"mountPath"`
	SecretPath string `json:"secretPath"`
	SecretKey  string `json:"secretKey"`
}

// +kubebuilder:object:root=true

// GithubAppList contains a list of GithubApp
type GithubAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GithubApp{}, &GithubAppList{})
}

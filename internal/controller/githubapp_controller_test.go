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
	"time"
	"os"
	"fmt"
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	githubappv1 "github-app-operator/api/v1"
)


var _ = Describe("GithubApp controller", func() {

	const (
		privateKeySecret     = "gh-app-key-test"
		sourceNamespace      = "default"
		appId				 = 857468
		installId			 = 48531286
		githubAppName		 = "gh-app-test"
	)

	var privateKey           = os.Getenv("GITHUB_PRIVATE_KEY")

	Context("When setting up the test environment", func() {
		It("Should create GithubApp custom resources", func() {
			By("Creating the privateKeySecret in the sourceNamespace")

			ctx := context.Background()

			secret1Obj := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:		privateKeySecret,
					Namespace: 	sourceNamespace,
				},
				Data: map[string][]byte{"privateKey": []byte(privateKey)},
			}
			Expect(k8sClient.Create(ctx, &secret1Obj)).Should(Succeed())

			By("Creating a first GithubApp custom resource in the sourceNamespace")
			githubApp := githubappv1.GithubApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      githubAppName,
					Namespace: sourceNamespace,
				},
				Spec: githubappv1.GithubAppSpec{
					AppId: appId,
					InstallId: installId,
					PrivateKeySecret: privateKeySecret,
				},
			}
			Expect(k8sClient.Create(ctx, &githubApp)).Should(Succeed())
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("Should retrieve the private key from the secret", func() {
			ctx := context.Background()

			// Retrieve the privateKeySecret from the cluster
			secretKey := types.NamespacedName{Name: privateKeySecret, Namespace: sourceNamespace}
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, secretKey, secret)).To(Succeed())

			// Retrieve the privateKey data from the secret
			privateKeyBytes, ok := secret.Data["privateKey"]
			Expect(ok).To(BeTrue(), "privateKey data not found in secret")

			// Convert privateKey bytes to string
			privateKey = string(privateKeyBytes)
			Expect(privateKey).NotTo(BeEmpty(), "privateKey is empty")

			fmt.Println("Private Key:", privateKey)
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("Should create a Secret with the access token", func() {
			ctx := context.Background()

			// Define the expected secret name
			secretName := fmt.Sprintf("github-app-access-token-857468")

			var retrievedSecret corev1.Secret

			// Wait for the Secret to be created
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: sourceNamespace}, &retrievedSecret)
				return err == nil
			}, "60s", "5s").Should(BeTrue(), fmt.Sprintf("Expected Secret %s/%s not created", sourceNamespace, secretName))

			// Retrieve the access token from the Secret
			retrievedAccessToken, found := retrievedSecret.StringData["accessToken"]
			Expect(found).To(BeTrue(), fmt.Sprintf("Expected access token not found in Secret %s/%s", sourceNamespace, secretName))
			Expect(retrievedAccessToken).NotTo(BeEmpty(), fmt.Sprintf("Retrieved access token from Secret %s/%s is empty", sourceNamespace, secretName))
			fmt.Println("Access Token:", retrievedAccessToken)
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("Should write the access token to a secret and set the GithubApp expiresAt status", func() {
			By("Checking if the GithubApp status has changed to the right status")
			ctx := context.Background()
			// Retrieve the GithubApp object to check its status
			key := types.NamespacedName{Name: githubAppName, Namespace: sourceNamespace}
			retrievedGithubApp := &githubappv1.GithubApp{}
			timeout := 60 * time.Second
			interval := 5 * time.Second
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, retrievedGithubApp); err != nil {
					return false
				}
				fmt.Println("GithubApp Status:", retrievedGithubApp.Status.ExpiresAt.Time)

				// Check if the expiresAt field is not zero or empty
				return !retrievedGithubApp.Status.ExpiresAt.Time.IsZero()
			}, timeout, interval).Should(BeTrue(), "GithubApp expiresAt status not set or invalid")
		})
	})

})

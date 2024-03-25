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
	"fmt"
	"reflect"
	"time"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	githubappv1 "github-app-operator/api/v1"
)

var _ = Describe("GithubApp controller", func() {

	const (
		privateKeySecret     = "gh-app-key-test"
		sourceNamespace      = "default"
		appId				 = "857468"
		installId			 = "48531286"
		githubAppName		 = "gh-app-test"
	)

	var privateKey           = os.Getenv("GITHUB_PRIVATE_KEY")

	Context("When setting up the test environment", func() {
		It("Should create GithubApp custom resources", func() {
			By("Creating the privateKeySecret in the sourceNamespace")
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
				Spec: syncv1.GithubAppSpec{
					AppId: appId,
					InstallId: installId,
					PrivateKeySecret: privateKeySecret,
				},
			}
			Expect(k8sClient.Create(ctx, &githubApp)).Should(Succeed())
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
				// Check if the Secrets field matches the expected value
				return reflect.DeepEqual(retrievedGithubApp.Status.ExpiresAt, true)
			}, timeout, interval).Should(BeTrue(), "GithubApp status didn't change to the right status")
		})
	})
})

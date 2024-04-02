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
	"os"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	test_helpers "github-app-operator/internal/controller/test_helpers"

	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	privateKeySecret = "gh-app-key-test"
	appId            = 857468
	installId        = 48531286
	githubAppName    = "gh-app-test"
	githubAppName2   = "gh-app-test-2"
	githubAppName3   = "gh-app-test-3"
	githubAppName4   = "gh-app-test-4"
	namespace1       = "default"
	namespace2       = "namespace2"
	namespace3       = "namespace3"
	namespace4       = "namespace4"
)

var (
	acessTokenSecretName = fmt.Sprintf("github-app-access-token-%s", strconv.Itoa(appId))
)
	
// Tests
var _ = Describe("GithubApp controller", func() {

	Context("When setting up the test environment", func() {
		It("Should create GithubApp custom resources", func() {
			ctx := context.Background()

			By("Creating the privateKeySecret in the namespace1")
			test_helpers.CreatePrivateKeySecret(ctx, namespace1, "privateKey")

			By("Creating a first GithubApp custom resource in the namespace1")
			test_helpers.CreateGitHubAppAndWait(ctx, namespace1, githubAppName, nil)
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("should successfully reconcile the resource", func() {
			ctx := context.Background()

			By("Waiting for the access token secret to be created")
			test_helpers.WaitForAccessTokenSecret(ctx, namespace1)
		})
	})

	Context("When deleting an access token secret", func() {
		It("should successfully reconcile the secret again", func() {
			ctx := context.Background()

			By("Deleting the access token secret")
			err := k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      acessTokenSecretName,
					Namespace: namespace1,
				},
			})
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf(
				"Failed to delete Secret %s/%s: %v",
				namespace1,
				acessTokenSecretName,
				err,
				),
			)

			By("Waiting for the access token secret to be created")
			test_helpers.WaitForAccessTokenSecret(ctx, namespace1)
		})
	})

	Context("When manually changing accessToken secret to an invalid value", func() {
		It("Should update the accessToken on reconciliation", func() {
			ctx := context.Background()

			By("Modifying the access token secret with an invalid token")
			dummyAccessToken := "dummy_access_token"
			accessTokenSecretKey := test_helpers.UpdateAccessTokenSecret(ctx, namespace1, "token", dummyAccessToken)

			// Wait for the accessToken to be updated
			Eventually(func() string {
				updatedSecret := &corev1.Secret{}
				err := k8sClient.Get(ctx, accessTokenSecretKey, updatedSecret)
				Expect(err).To(Succeed())
				return string(updatedSecret.Data["token"])
			}, "60s", "5s").ShouldNot(Equal(dummyAccessToken))
		})
	})

	Context("When adding an invalid key to the accessToken secret", func() {
		It("Should update the accessToken secret and remove the invalid key on reconciliation", func() {
			ctx := context.Background()

			By("Modifying the access token secret with an invalid key")
			accessTokenSecretKey := test_helpers.UpdateAccessTokenSecret(ctx, namespace1, "foo", "dummy_value")

			// Wait for the accessToken to be updated and the "foo" key to be removed
			Eventually(func() []byte {
				updatedSecret := &corev1.Secret{}
				err := k8sClient.Get(ctx, accessTokenSecretKey, updatedSecret)
				Expect(err).To(Succeed())
				return updatedSecret.Data["foo"]
			}, "60s", "5s").Should(BeNil())
		})
	})

	Context("When requeing a reconcile for a GithubApp that is not expired", func() {
		It("should successfully reconcile the resource and get the rate limit", func() {
			ctx := context.Background()

			By("Reconciling the created resource")
			controllerReconciler := &GithubAppReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Perform reconciliation for the resource
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: namespace1,
					Name:      githubAppName,
				},
			})

			// Verify if reconciliation was successful
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Reconciliation failed: %v", err))

			// Print the result
			fmt.Println("Reconciliation result:", result)

			// Delete the GitHubApp after reconciliation
			test_helpers.DeleteGitHubAppAndWait(ctx, namespace1, githubAppName)
		})
	})

	Context("When reconciling a GithubApp with spec.restartPods.labels.foo as bar", func() {
		It("Should eventually delete the pod with the matching label foo: bar", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			test_helpers.CreateNamespace(ctx, namespace2)

			By("Creating the privateKeySecret in namespace2")
			test_helpers.CreatePrivateKeySecret(ctx, namespace2, "privateKey")

			By("Creating a pod with the label foo: bar")
			pod1 := test_helpers.CreatePodWithLabel(ctx, "foo", namespace2, "foo", "bar")

			By("Creating a pod with the label foo: bar2")
			pod2 := test_helpers.CreatePodWithLabel(ctx, "foo", namespace2, "foo", "bar2")

			By("Creating a GithubApp with the spec.restartPods.labels foo: bar")
			restartPodsSpec := &githubappv1.RestartPodsSpec{
				Labels: map[string]string{
					"foo": "bar",
				},
			}
			// Create a GithubApp instance with the RestartPods field initialized
			test_helpers.CreateGitHubAppAndWait(ctx, namespace2, githubAppName2, restartPodsSpec)

			By("Waiting for pod1 with the label 'foo: bar' to be deleted")
			// Wait for the pod to be deleted by the reconcile loop
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: pod1.Name, Namespace: pod1.Namespace}, pod1)
				return apierrors.IsNotFound(err) // Pod is deleted
			}, "60s", "5s").Should(BeTrue(), "Failed to delete the pod within timeout")

			By("Checking pod2 with the label 'foo: bar2' still exists and not marked for deletion")
			Consistently(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: pod2.Name, Namespace: pod2.Namespace}, pod2)
				if err != nil && apierrors.IsNotFound(err) {
					// Pod2 is deleted
					return false
				}
				// Check if pod2 has a deletion timestamp
				return pod2.DeletionTimestamp == nil
			}, "10s", "2s").Should(BeTrue(), "Pod2 is marked for deletion")
	
			// Delete pod2
			err := k8sClient.Delete(ctx, pod2)
			Expect(err).ToNot(HaveOccurred(), "Failed to delete pod2: %v", err)

			// Delete the GitHubApp after reconciliation
			test_helpers.DeleteGitHubAppAndWait(ctx, namespace2, githubAppName2)
		})
	})

	Context("When reconciling a GithubApp with an app secret with no privateKey field", func() {
		It("Should raise an error message 'privateKey not found in Secret'", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			test_helpers.CreateNamespace(ctx, namespace4)

			By("Creating the privateKeySecret in namespace4 without the 'privateKey' field")
			test_helpers.CreatePrivateKeySecret(ctx, namespace4, "foo")

			By("Creating a GithubApp without creating the privateKeySecret with 'privateKey' field")
			test_helpers.CreateGitHubAppAndWait(ctx, namespace4, githubAppName4, nil)

			By("Checking the githubApp `status.error` value is as expected")
			test_helpers.CheckGithubAppStatusError(ctx, githubAppName4, namespace4, "privateKey not found in Secret")

			// Delete the GitHubApp after reconciliation
			test_helpers.DeleteGitHubAppAndWait(ctx, namespace4, githubAppName4)
		})
	})

	Context("When reconciling a GithubApp with an error", func() {
		It("Should reflect the error message in the status.Error field of the object", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			test_helpers.CreateNamespace(ctx, namespace3)

			By("Creating a GithubApp without creating the privateKeySecret")
			test_helpers.CreateGitHubAppAndWait(ctx, namespace3, githubAppName3, nil)

			By("Checking the githubApp `status.error` value is as expected")
			test_helpers.CheckGithubAppStatusError(ctx, githubAppName3, namespace3, "Secret \"gh-app-key-test\" not found")
		})
	})

	Context("When reconciling a GithubApp that is in error state after fixing the error", func() {
		It("Should reflect reconcile with no errors and clear the `status.error` field", func() {
			ctx := context.Background()

			By("Creating the privateKeySecret in namespace3")
			test_helpers.CreatePrivateKeySecret(ctx, namespace3, "privateKey")

			By("Waiting for the access token secret to be created")
			test_helpers.WaitForAccessTokenSecret(ctx, namespace3)

			By("Checking the githubApp `status.error` value is as expected")
			test_helpers.CheckGithubAppStatusError(ctx, githubAppName3, namespace3, "")

			// Delete the GitHubApp after reconciliation
			test_helpers.DeleteGitHubAppAndWait(ctx, namespace3, githubAppName3)
		})
	})
})

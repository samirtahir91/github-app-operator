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
	"os"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
	podName          = "foo"
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
	privateKey = os.Getenv("GITHUB_PRIVATE_KEY")
	acessTokenSecretName = fmt.Sprintf("github-app-access-token-%s", strconv.Itoa(appId))
)

// Function to delete a GitHubApp and wait for its deletion
func deleteGitHubAppAndWait(ctx context.Context, namespace, name string) {
	// Delete the GitHubApp
	err := k8sClient.Delete(ctx, &githubappv1.GithubApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to delete GitHubApp: %v", err))

	// Wait for the GitHubApp to be deleted
	Eventually(func() bool {
		// Check if the GitHubApp still exists
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, &githubappv1.GithubApp{})
		return apierrors.IsNotFound(err) // GitHubApp is deleted
	}, "60s", "5s").Should(BeTrue(), "Failed to delete GitHubApp within timeout")
}

// Function to create a GitHubApp and wait for its creation
func createGitHubAppAndWait(ctx context.Context, namespace, name string, restartPodsSpec *githubappv1.RestartPodsSpec) {
	// create the GitHubApp
	githubApp := githubappv1.GithubApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: githubappv1.GithubAppSpec{
			AppId:            appId,
			InstallId:        installId,
			PrivateKeySecret: privateKeySecret,
			RestartPods:      restartPodsSpec, // Optionally pass restartPods
		},
	}
	Expect(k8sClient.Create(ctx, &githubApp)).Should(Succeed())
}

// Function to create a privateKey Secret and wait for its creation
func createPrivateKeySecret(ctx context.Context, namespace string, key string) {

	// Decode base64-encoded private key
	decodedPrivateKey, err := base64.StdEncoding.DecodeString(privateKey)
	Expect(err).NotTo(HaveOccurred(), "error decoding base64-encoded private key")

	// create the GitHubApp
	secret1Obj := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      privateKeySecret,
			Namespace: namespace,
		},
		Data: map[string][]byte{key: decodedPrivateKey},
	}
	Expect(k8sClient.Create(ctx, &secret1Obj)).Should(Succeed())
}

// Function to create a namespace
func createNamespace(ctx context.Context, namespace string) {

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
}

// Function to wait for access token secret to be created
func waitForAccessTokenSecret(ctx context.Context, namespace string) {
	var retrievedSecret corev1.Secret
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: acessTokenSecretName, Namespace: namespace}, &retrievedSecret)
		return err == nil
	}, "20s", "5s").Should(BeTrue(), fmt.Sprintf("Access token secret %s/%s not created", namespace, acessTokenSecretName))
}

// Function to update access token secret data with dummy data
func updateAccessTokenSecret(ctx context.Context, namespace string, key string, dummyKeyValue string)  types.NamespacedName {
	// Update the accessToken to a dummy value
	accessTokenSecretKey := types.NamespacedName{
		Namespace: namespace,
		Name:      acessTokenSecretName,
	}
	accessTokenSecret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, accessTokenSecretKey, accessTokenSecret)).To(Succeed())
	accessTokenSecret.Data[key] = []byte(dummyKeyValue)
	Expect(k8sClient.Update(ctx, accessTokenSecret)).To(Succeed())

	return accessTokenSecretKey
}

// Function to validate an err message from a githubApp
func checkGithubAppStatusError(ctx context.Context, githubAppName string, namespace string, errMsg string) {

	// Check if the status.Error field gets populated with the expected error message
	Eventually(func() bool {
		// Retrieve the GitHubApp object
		key := types.NamespacedName{Name: githubAppName, Namespace: namespace}
		retrievedGithubApp := &githubappv1.GithubApp{}
		err := k8sClient.Get(ctx, key, retrievedGithubApp)
		if err != nil {
			return false // Unable to retrieve the GitHubApp
		}
		// Check if the status.Error field contains the expected error message
		return retrievedGithubApp.Status.Error == errMsg
	}, "30s", "5s").Should(BeTrue(), "Failed to set status.Error field within timeout")
}
		
// Tests
var _ = Describe("GithubApp controller", func() {

	Context("When setting up the test environment", func() {
		It("Should create GithubApp custom resources", func() {
			ctx := context.Background()

			By("Creating the privateKeySecret in the namespace1")
			createPrivateKeySecret(ctx, namespace1, "privateKey")

			By("Creating a first GithubApp custom resource in the namespace1")
			createGitHubAppAndWait(ctx, namespace1, githubAppName, nil)
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("should successfully reconcile the resource", func() {
			ctx := context.Background()

			By("Waiting for the access token secret to be created")
			waitForAccessTokenSecret(ctx, namespace1)
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
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to delete Secret %s/%s: %v", namespace1, acessTokenSecretName, err))

			By("Waiting for the access token secret to be created")
			waitForAccessTokenSecret(ctx, namespace1)
		})
	})

	Context("When manually changing accessToken secret to an invalid value", func() {
		It("Should update the accessToken on reconciliation", func() {
			ctx := context.Background()

			By("Modifying the access token secret with an invalid token")
			// Define constants for test
			dummyAccessToken := "dummy_access_token"
			accessTokenSecretKey := updateAccessTokenSecret(ctx, namespace1, "token", dummyAccessToken)

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
			// Define constants for test
			dummyKeyValue := "dummy_value"
			accessTokenSecretKey := updateAccessTokenSecret(ctx, namespace1, "foo", dummyKeyValue)

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
			deleteGitHubAppAndWait(ctx, namespace1, githubAppName)
		})
	})

	Context("When reconciling a GithubApp with spec.restartPods.labels.foo as bar", func() {
		It("Should eventually delete the pod with the matching label foo: bar", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			createNamespace(ctx, namespace2)

			By("Creating the privateKeySecret in namespace2")
			createPrivateKeySecret(ctx, namespace2, "privateKey")

			By("Creating a pod with the label foo: bar")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: podName,
					Namespace:    namespace2,
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  podName,
							Image: "busybox",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

			By("Creating a GithubApp with the spec.restartPods.labels foo: bar")
			restartPodsSpec := &githubappv1.RestartPodsSpec{
				Labels: map[string]string{
					"foo": "bar",
				},
			}
			// Create a GithubApp instance with the RestartPods field initialized
			createGitHubAppAndWait(ctx, namespace2, githubAppName2, restartPodsSpec) // With restartPods

			// Wait for the pod to be deleted by the reconcile loop
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
				return apierrors.IsNotFound(err) // Pod is deleted
			}, "60s", "5s").Should(BeTrue(), "Failed to delete the pod within timeout")

			// Delete the GitHubApp after reconciliation
			deleteGitHubAppAndWait(ctx, namespace2, githubAppName2)
		})
	})

	Context("When reconciling a GithubApp with an app secret with no privateKey field", func() {
		It("Should raise an error message 'privateKey not found in Secret'", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			createNamespace(ctx, namespace4)

			By("Creating the privateKeySecret in namespace4 without the 'privateKey' field")
			createPrivateKeySecret(ctx, namespace4, "foo")

			By("Creating a GithubApp without creating the privateKeySecret with 'privateKey' field")
			createGitHubAppAndWait(ctx, namespace4, githubAppName4, nil)

			By("Checking the githubApp `status.error` value is as expected")
			checkGithubAppStatusError(ctx, githubAppName4, namespace4, "privateKey not found in Secret")

			// Delete the GitHubApp after reconciliation
			deleteGitHubAppAndWait(ctx, namespace4, githubAppName4)
		})
	})

	Context("When reconciling a GithubApp with an error", func() {
		It("Should reflect the error message in the status.Error field of the object", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			createNamespace(ctx, namespace3)

			By("Creating a GithubApp without creating the privateKeySecret")
			createGitHubAppAndWait(ctx, namespace3, githubAppName3, nil)

			By("Checking the githubApp `status.error` value is as expected")
			checkGithubAppStatusError(ctx, githubAppName3, namespace3, "Secret \"gh-app-key-test\" not found")
		})
	})

	Context("When reconciling a GithubApp that is in error state after fixing the error", func() {
		It("Should reflect reconcile with no errors and clear the `status.error` field", func() {
			ctx := context.Background()

			By("Creating the privateKeySecret in namespace3")
			createPrivateKeySecret(ctx, namespace3, "privateKey")

			By("Waiting for the access token secret to be created")
			waitForAccessTokenSecret(ctx, namespace3)

			By("Checking the githubApp `status.error` value is as expected")
			checkGithubAppStatusError(ctx, githubAppName3, namespace3, "")

			// Delete the GitHubApp after reconciliation
			deleteGitHubAppAndWait(ctx, namespace3, githubAppName3)
		})
	})
})

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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	githubappv1 "github-app-operator/api/v1"
)

const (
	privateKeySecret = "gh-app-key-test"
	namespace1  	 = "default"
	appId            = 857468
	installId        = 48531286
	githubAppName    = "gh-app-test"
	githubAppName2   = "gh-app-test-2"
	githubAppName3   = "gh-app-test-3"
	githubAppName4   = "gh-app-test-4"
	podName          = "foo"
	namespace2       = "namespace2"
	namespace3       = "namespace3"
	namespace4       = "namespace4"
)

var (
	privateKey = os.Getenv("GITHUB_PRIVATE_KEY")
	secretName = fmt.Sprintf("github-app-access-token-%s", strconv.Itoa(appId))
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

var _ = Describe("GithubApp controller", func() {

	Context("When setting up the test environment", func() {
		It("Should create GithubApp custom resources", func() {
			By("Creating the privateKeySecret in the namespace1")
			ctx := context.Background()
			// Create private key secret
			createPrivateKeySecret(ctx, namespace1, "privateKey")

			By("Creating a first GithubApp custom resource in the namespace1")
			createGitHubAppAndWait(ctx, namespace1, githubAppName, nil)
		})
	})

	Context("When reconciling a GithubApp", func() {
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			ctx := context.Background()

			By("Retrieving the access token secret")

			var retrievedSecret corev1.Secret

			// Wait for the Secret to be created
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace1}, &retrievedSecret)
				return err == nil
			}, "20s", "5s").Should(BeTrue(), fmt.Sprintf("Access token secret %s/%s not created", namespace1, secretName))

		})
	})

	Context("When deleting an access token secret", func() {
		It("should successfully reconcile the secret again", func() {
			By("Deleting the access token secret")
			ctx := context.Background()

			var retrievedSecret corev1.Secret

			// Delete the Secret if it exists
			err := k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace1,
				},
			})
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to delete Secret %s/%s: %v", namespace1, secretName, err))

			// Wait for the Secret to be recreated
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace1}, &retrievedSecret)
				return err == nil
			}, "30s", "5s").Should(BeTrue(), fmt.Sprintf("Expected Secret %s/%s not recreated", namespace1, secretName))
		})
	})

	Context("When manually changing accessToken secret to an invalid value", func() {
		It("Should update the accessToken on reconciliation", func() {
			ctx := context.Background()

			// Define constants for test
			dummyAccessToken := "dummy_access_token"

			// Edit the accessToken to a dummy value
			accessTokenSecretKey := types.NamespacedName{
				Namespace: namespace1,
				Name:      secretName,
			}
			accessTokenSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, accessTokenSecretKey, accessTokenSecret)).To(Succeed())
			accessTokenSecret.Data["token"] = []byte(dummyAccessToken)
			Expect(k8sClient.Update(ctx, accessTokenSecret)).To(Succeed())

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
	
			// Define constants for test
			dummyKeyValue := "dummy_value"
	
			// Edit the accessToken to a dummy value
			accessTokenSecretKey := types.NamespacedName{
				Namespace: namespace1,
				Name:      secretName,
			}
			accessTokenSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, accessTokenSecretKey, accessTokenSecret)).To(Succeed())
			accessTokenSecret.Data["foo"] = []byte(dummyKeyValue)
			Expect(k8sClient.Update(ctx, accessTokenSecret)).To(Succeed())
	
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
			By("Reconciling the created resource")
			ctx := context.Background()

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
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace2,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating the privateKeySecret in namespace2")
			// Create private key secret
			createPrivateKeySecret(ctx, namespace2, "privateKey")

			By("Creating a pod with the label foo: bar")
			// Create a pod with label "foo: bar"
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
			// Create a RestartPodsSpec instance
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
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace4,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating the privateKeySecret in namespace4 without the 'privateKey' field")
			// Create private key secret
			createPrivateKeySecret(ctx, namespace4, "foo")

			By("Creating a GithubApp without creating the privateKeySecret with 'privateKey' field")
			// Create a GithubApp instance
			createGitHubAppAndWait(ctx, namespace4, githubAppName4, nil)

			// Check if the status.Error field gets populated with the expected error message
			Eventually(func() bool {
				// Retrieve the GitHubApp object
				key := types.NamespacedName{Name: githubAppName4, Namespace: namespace4}
				retrievedGithubApp := &githubappv1.GithubApp{}
				err := k8sClient.Get(ctx, key, retrievedGithubApp)
				if err != nil {
					return false // Unable to retrieve the GitHubApp
				}
				// Check if the status.Error field contains the expected error message
				return retrievedGithubApp.Status.Error == "privateKey not found in Secret"
			}, "60s", "5s").Should(BeTrue(), "Failed to set status.Error field within timeout")

			// Delete the GitHubApp after reconciliation
			deleteGitHubAppAndWait(ctx, namespace4, githubAppName4)
		})
	})

	Context("When reconciling a GithubApp with an error", func() {
		It("Should reflect the error message in the status.Error field of the object", func() {
			ctx := context.Background()

			By("Creating a new namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace3,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			By("Creating a GithubApp without creating the privateKeySecret")
			// Create a GithubApp instance
			createGitHubAppAndWait(ctx, namespace3, githubAppName3, nil)

			// Check if the status.Error field gets populated with the expected error message
			Eventually(func() bool {
				// Retrieve the GitHubApp object
				key := types.NamespacedName{Name: githubAppName3, Namespace: namespace3}
				retrievedGithubApp := &githubappv1.GithubApp{}
				err := k8sClient.Get(ctx, key, retrievedGithubApp)
				if err != nil {
					return false // Unable to retrieve the GitHubApp
				}
				// Check if the status.Error field contains the expected error message
				return retrievedGithubApp.Status.Error == "Secret \"gh-app-key-test\" not found"
			}, "60s", "5s").Should(BeTrue(), "Failed to set status.Error field within timeout")
		})
	})

	Context("When reconciling a GithubApp that is in error state after fixing the error", func() {
		It("Should reflect reconcile with no errors and clear the `status.error` field", func() {
			ctx := context.Background()

			By("Creating the privateKeySecret in namespace3")
			// Create private key secret
			createPrivateKeySecret(ctx, namespace3, "privateKey")

			// Wait for the access token Secret to be recreated
			var retrievedSecret corev1.Secret
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace3}, &retrievedSecret)
				return err == nil
			}, "30s", "5s").Should(BeTrue(), fmt.Sprintf("Expected Secret %s/%s not recreated", namespace3, secretName))

			// Check if the status.Error field gets populated with the expected error message
			Eventually(func() bool {
				// Retrieve the GitHubApp object
				key := types.NamespacedName{Name: githubAppName3, Namespace: namespace3}
				retrievedGithubApp := &githubappv1.GithubApp{}
				err := k8sClient.Get(ctx, key, retrievedGithubApp)
				if err != nil {
					return false // Unable to retrieve the GitHubApp
				}
				// Check if the status.Error field has been cleared of errors
				return retrievedGithubApp.Status.Error == ""
			}, "30s", "5s").Should(BeTrue(), "Failed to clear status.Error field within timeout")

			// Delete the GitHubApp after reconciliation
			deleteGitHubAppAndWait(ctx, namespace3, githubAppName3)
		})
	})
})

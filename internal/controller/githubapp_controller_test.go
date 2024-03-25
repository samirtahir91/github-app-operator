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
	//"time"
	"os"
	"fmt"
	"encoding/base64"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

			// Decode base64-encoded private key
			decodedPrivateKey, err := base64.StdEncoding.DecodeString(privateKey)
			Expect(err).NotTo(HaveOccurred(), "error decoding base64-encoded private key")
			
			secret1Obj := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:		privateKeySecret,
					Namespace: 	sourceNamespace,
				},
				Data: map[string][]byte{"privateKey": []byte(decodedPrivateKey)},
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
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			ctx := context.Background()

			controllerReconciler := &GithubAppReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
	
			// Perform reconciliation for the resource
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: sourceNamespace,
					Name:      githubAppName,
				},
			})
	
			// Verify if reconciliation was successful
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Reconciliation failed: %v", err))
			// You can add more specific assertions here depending on your controller's reconciliation logic
			
			// Print the result
			fmt.Println("Reconciliation result:", result)
			// Add a sleep to allow the controller to trigger requeue
			time.Sleep(120 * time.Second)
		})
	})
})

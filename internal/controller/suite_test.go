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
	"net/http" // http client
	"os"

	//"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	vault "github.com/hashicorp/vault/api"   // vault client
	kubernetes "k8s.io/client-go/kubernetes" // k8s client
	ctrlConfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	test_helpers "github-app-operator/internal/controller/test_helpers"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	githubappv1 "github-app-operator/api/v1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg           *rest.Config
	k8sClient     client.Client
	httpClient    *http.Client
	vaultClient   *vault.Client
	k8sClientset  *kubernetes.Clientset
	testEnv       *envtest.Environment
	ctx           context.Context
	cancel        context.CancelFunc
	tokenFilePath = "/tmp/githubOperatorServiceAccountToken"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.28.3-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	// set check interval and expiry threshold for test env
	osEnvErr := os.Setenv("CHECK_INTERVAL", "15s")
	Expect(osEnvErr).NotTo(HaveOccurred())
	osEnvErr = os.Setenv("EXPIRY_THRESHOLD", "15m")
	Expect(osEnvErr).NotTo(HaveOccurred())
	osEnvErr = os.Setenv("DEBUG_LOG", "true")
	Expect(osEnvErr).NotTo(HaveOccurred())

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = githubappv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Register and start the SecretSync controller
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	// http client
	httpClient = &http.Client{}

	var token string
	if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
		// Initialise vault client with default config - uses default Vault env vars for config
		// See - https://pkg.go.dev/github.com/hashicorp/vault/api#pkg-constants
		vaultConfig := vault.DefaultConfig()
		vaultClient, err = vault.NewClient(vaultConfig)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Vault client initialisation failed: %v", err))

		// Initialise K8s client
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("Error initializing Kubernetes clientset:", r)
			}
		}()
		k8sClientset = kubernetes.NewForConfigOrDie(ctrlConfig.GetConfigOrDie())
		fmt.Println("Got main k8sClientset:", k8sClientset)

		// Create a valid service account token to initialise the controller SetupWithManager with
		// This will be used in Token Request API
		By("Creating a new namespace")
		test_helpers.CreateNamespace(ctx, k8sClient, "namespace0")
		time.Sleep(5 * time.Second)

		By("Creating a new token via token request")
		controllerReconciler := &GithubAppReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			K8sClient: k8sClientset,
		}

		vaultAudience := "githubapp"

		token, err = controllerReconciler.RequestToken(ctx, vaultAudience, "namespace0", "default")
		// Verify if reconciliation was successful
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Token request failed: %v", err))
	} else {
		// Set a dummy token just to satisfy the SetupWithManager function
		// which will read the token and get the service account and and namespace.
		// This token is for 'default' service account in the 'namespace0' namespace
		token = `eyJhbGciOiJSUzI1NiIsImtpZCI6Ik5ieTJyVUk2ZzlQZ0k0anNGclRvTkJDM0FsUjJjLUJDVUhzNU9mVG9lcEUifQ.
		eyJhdWQiOlsiZ2l0aHViYXBwIl0sImV4cCI6MTcxMzEyNjIxMiwiaWF0IjoxNzEzMTI1NjEyLCJpc3MiOiJodHRwczovL2
		t1YmVybmV0ZXMuZGVmYXVsdC5zdmMuY2x1c3Rlci5sb2NhbCIsImt1YmVybmV0ZXMuaW8iOnsibmFtZXNwYWNlIjoibmFtZ
		XNwYWNlMCIsInNlcnZpY2VhY2NvdW50Ijp7Im5hbWUiOiJkZWZhdWx0IiwidWlkIjoiNDY3ZTA4MGMtYWZhNy00OTc4LWFkY
		zMtYWI5NmFkMWJjOTQzIn19LCJuYmYiOjE3MTMxMjU2MTIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpuYW1lc3BhY2
		UwOmRlZmF1bHQifQ.ftFKNIwM_qi-6W7rvMyjNC2xAbNfFsrRgPQnjEsDw84_fpn9I1LFnQPQzA5HeTtJyIBzjrdHsEGgcCTx
		sYLgErLkIJ9MfWxwyP3FsNeuQgNoBr4Pmo9lRnayzERU9YZwEb9QCoZXGkCrcv16q15hB_J_ik9lcwlLJ6PYDW58AA39VUDs
		fqin-8D23ghmAumv8vods6v-WVNeMKAP0oO7oqElLop9r5h8hf9ApaAZ2zRbTnQ-X1HpAFwOzRTGvcPli1hLZ7rgDAw6yJOOn
		ExPgMZ44umiaXVhnow2Vxol2G7yb0mFToWOwpiHZPsL4kZccGK33nk1Kcfcawqqd2IGBA`
	}

	var file *os.File
	file, err = os.Create(tokenFilePath)
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Println("Error closing file:", err)
		}
	}()
	_, err = file.WriteString(token)
	Expect(err).ToNot(HaveOccurred())

	// Path to store private keys for local caching
	privateKeyCachePath := "/tmp/github-app-operator/"
	// Remove private key cache
	err = os.RemoveAll(privateKeyCachePath)
	Expect(err).NotTo(HaveOccurred())

	err = (&GithubAppReconciler{
		Client:      k8sManager.GetClient(),
		Scheme:      k8sManager.GetScheme(),
		Recorder:    k8sManager.GetEventRecorderFor("githubapp-controller"),
		HTTPClient:  httpClient,
		VaultClient: vaultClient,
		K8sClient:   k8sClientset,
	}).SetupWithManager(k8sManager, privateKeyCachePath, tokenFilePath)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	// Remove service account token file
	err = os.Remove(tokenFilePath)
	Expect(err).NotTo(HaveOccurred())
	// Remove private key cache
	err = os.RemoveAll(privateKeyCachePath)
	Expect(err).NotTo(HaveOccurred())
})

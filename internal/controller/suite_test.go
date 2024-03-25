package controller

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	githubappv1 "github-app-operator/api/v1"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

// Define a buffer to capture logs
var logBuffer bytes.Buffer

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	// Redirect logs before running tests
	logf.SetLogger(zap.New(zap.WriteTo(&logBuffer), zap.UseDevMode(true)))

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = githubappv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())

	// Print logs after running tests
	fmt.Println("Controller Logs:")
	fmt.Println(logBuffer.String())
})

var _ = ginkgo.BeforeEach(func() {
	// Clear the log buffer before each test
	logBuffer.Reset()
})

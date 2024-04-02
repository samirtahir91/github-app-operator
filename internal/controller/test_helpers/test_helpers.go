// test_helpers.go

package test_helpers

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"

    gomega "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"
	githubappv1 "github-app-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// github app details
	privateKeySecret = "gh-app-key-test"
	appId            = 857468
	installId        = 48531286
)

var (
	privateKey = os.Getenv("GITHUB_PRIVATE_KEY")
	acessTokenSecretName = fmt.Sprintf("github-app-access-token-%s", strconv.Itoa(appId))
)

// Function to delete accessToken Secret
func DeleteAccessTokenSecret(ctx context.Context, k8sClient client.Client, namespace string) {
	err := k8sClient.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      acessTokenSecretName,
			Namespace: namespace,
		},
	})
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), fmt.Sprintf(
		"Failed to delete Secret %s/%s: %v",
		namespace,
		acessTokenSecretName,
		err,
		),
	)
}

// Function to delete a GitHubApp and wait for its deletion
func DeleteGitHubAppAndWait(ctx context.Context, k8sClient client.Client, namespace string, name string) {
	// Delete the GitHubApp
	err := k8sClient.Delete(ctx, &githubappv1.GithubApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to delete GitHubApp: %v", err))

	// Wait for the GitHubApp to be deleted
	gomega.Eventually(func() bool {
		// Check if the GitHubApp still exists
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, &githubappv1.GithubApp{})
		return apierrors.IsNotFound(err) // GitHubApp is deleted
	}, "60s", "5s").Should(gomega.BeTrue(), "Failed to delete GitHubApp within timeout")
}

// Function to create a GitHubApp and wait for its creation
func CreateGitHubAppAndWait(
	ctx context.Context,
	k8sClient client.Client,
	namespace,
	name string,
	restartPodsSpec *githubappv1.RestartPodsSpec,
	) {
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
	gomega.Expect(k8sClient.Create(ctx, &githubApp)).Should(gomega.Succeed())
}

// Function to create a privateKey Secret and wait for its creation
func CreatePrivateKeySecret(ctx context.Context, k8sClient client.Client, namespace string, key string) {

	// Decode base64-encoded private key
	decodedPrivateKey, err := base64.StdEncoding.DecodeString(privateKey)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "error decoding base64-encoded private key")

	// create the GitHubApp
	secret1Obj := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      privateKeySecret,
			Namespace: namespace,
		},
		Data: map[string][]byte{key: decodedPrivateKey},
	}
	gomega.Expect(k8sClient.Create(ctx, &secret1Obj)).Should(gomega.Succeed())
}

// Function to create a namespace
func CreateNamespace(ctx context.Context, k8sClient client.Client, namespace string) {

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	gomega.Expect(k8sClient.Create(ctx, ns)).Should(gomega.Succeed())
}

// Function to wait for access token secret to be created
func WaitForAccessTokenSecret(ctx context.Context, k8sClient client.Client, namespace string) {
	var retrievedSecret corev1.Secret
	gomega.Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: acessTokenSecretName,
			Namespace: namespace,
			},
			&retrievedSecret,
		)
		return err == nil
	}, "20s", "5s").Should(gomega.BeTrue(), fmt.Sprintf(
		"Access token secret %s/%s not created",
		namespace,
		acessTokenSecretName,
		),
	)
}

// Function to update access token secret data with dummy data
func UpdateAccessTokenSecret(
	ctx context.Context,
	k8sClient client.Client,
	namespace string,
	key string,
	dummyKeyValue string,
	)  types.NamespacedName {
	// Update the accessToken to a dummy value
	accessTokenSecretKey := types.NamespacedName{
		Namespace: namespace,
		Name:      acessTokenSecretName,
	}
	accessTokenSecret := &corev1.Secret{}
	gomega.Expect(k8sClient.Get(ctx, accessTokenSecretKey, accessTokenSecret)).To(gomega.Succeed())
	accessTokenSecret.Data[key] = []byte(dummyKeyValue)
	gomega.Expect(k8sClient.Update(ctx, accessTokenSecret)).To(gomega.Succeed())

	return accessTokenSecretKey
}

// Function to validate an err message from a githubApp
func CheckGithubAppStatusError(
	ctx context.Context,
	k8sClient client.Client,
	githubAppName string,
	namespace string,
	errMsg string,
	) {

	// Check if the status.Error field gets populated with the expected error message
	gomega.Eventually(func() bool {
		// Retrieve the GitHubApp object
		key := types.NamespacedName{Name: githubAppName, Namespace: namespace}
		retrievedGithubApp := &githubappv1.GithubApp{}
		err := k8sClient.Get(ctx, key, retrievedGithubApp)
		if err != nil {
			return false // Unable to retrieve the GitHubApp
		}
		// Check if the status.Error field contains the expected error message
		return retrievedGithubApp.Status.Error == errMsg
	}, "30s", "5s").Should(gomega.BeTrue(), "Failed to set status.Error field within timeout")
}

// Funtion to create a busybox pod with a label
func CreatePodWithLabel(
	ctx context.Context,
	k8sClient client.Client,
	podName string,
	namespace string,
	labeKey string,
	labelValue string,
	) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: podName,
			Namespace:    namespace,
			Labels: map[string]string{
				labeKey: labelValue,
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
	gomega.Expect(k8sClient.Create(ctx, pod)).Should(gomega.Succeed())

	return pod
}
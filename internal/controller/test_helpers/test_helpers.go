// test_helpers.go

package test_helpers

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	gomega "github.com/onsi/gomega"

	githubappv1 "github-app-operator/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// github app private key secret
	privateKeySecret = "gh-app-key-test"
)

var (
	privateKey           = os.Getenv("GITHUB_PRIVATE_KEY")
	appId                int
	installId            int
	acessTokenSecretName string
)

// Function to initialise vars for github app
func init() {
	var err error
	appId, err = strconv.Atoi(os.Getenv("GH_APP_ID"))
	if err != nil {
		panic(err)
	}
	installId, err = strconv.Atoi(os.Getenv("GH_INSTALL_ID"))
	if err != nil {
		panic(err)
	}
	acessTokenSecretName = fmt.Sprintf("github-app-access-token-%s", strconv.Itoa(appId))
}

// Function to check and wait for an event on a GithubApp object
func CheckEvent(
	ctx context.Context,
	k8sClient client.Client,
	githubAppName string,
	namespace string,
	eventType string,
	reason string,
	message string,
) {
	listOptions := &client.ListOptions{
		Namespace: namespace,
	}

	// Event not found, wait for it
	gomega.Eventually(func() error {
		// list events
		eventList := &corev1.EventList{}
		err := k8sClient.List(ctx, eventList, listOptions)
		if err != nil {
			return fmt.Errorf("failed to list events: %v", err)
		}
		// Check the event exists
		for _, evt := range eventList.Items {
			if evt.InvolvedObject.Name == githubAppName &&
				evt.Type == eventType &&
				evt.Reason == reason &&
				strings.Contains(evt.Message, message) {
				return nil // Event found
			}
		}

		// Event not found yet
		return fmt.Errorf("matching event not found")
	}, "20s", "5s").Should(gomega.Succeed())
}

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
	}, "20s", "5s").Should(gomega.BeTrue(), "Failed to delete GitHubApp within timeout")
}

// Function to create a GitHubApp and wait for its creation
func CreateGitHubAppAndWait(
	ctx context.Context,
	k8sClient client.Client,
	namespace,
	name string,
	rolloutDeploymentSpec *githubappv1.RolloutDeploymentSpec,
	vaultPrivateKeySpec *githubappv1.VaultPrivateKeySpec,
) {
	// create the GitHubApp
	githubApp := githubappv1.GithubApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: githubappv1.GithubAppSpec{
			AppId:             appId,
			InstallId:         installId,
			PrivateKeySecret:  privateKeySecret,
			RolloutDeployment: rolloutDeploymentSpec, // Optionally pass rolloutDeployment
			VaultPrivateKey:   vaultPrivateKeySpec,   // Optionally pass vaultPrivateKeySpec
			AccessTokenSecret: acessTokenSecretName,
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
			Name:      acessTokenSecretName,
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
) types.NamespacedName {
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

/*
Function to create a Deployment with a pod template and label
This will only work on a real cluster and NOT envtest since
it requires the Deployment controller
*/
func CreateDeploymentWithLabel(
	ctx context.Context,
	k8sClient client.Client,
	deploymentName string,
	namespace string,
	labelKey string,
	labelValue string,
) (*appsv1.Deployment, *corev1.Pod) {

	// just create 1 replica
	replicas := int32(1)

	// Pod template
	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": deploymentName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  deploymentName,
					Image: "busybox",
					Command: []string{
						"sleep",
						"1d", // keep-alive for tests
					},
				},
			},
		},
	}

	// Deployment spec
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				labelKey: labelValue,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": deploymentName,
				},
			},
			Template: podTemplate,
		},
	}

	// Create the Deployment
	gomega.Expect(k8sClient.Create(ctx, deployment)).Should(gomega.Succeed())

	// Create a list options with label selector
	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{"app": deploymentName}),
	}
	podList := &corev1.PodList{}
	// Wait for the pod list to be populated
	gomega.Eventually(func() []corev1.Pod {
		gomega.Expect(k8sClient.List(ctx, podList, listOptions)).Should(gomega.Succeed())
		return podList.Items
	}, "30s", "5s").ShouldNot(gomega.BeEmpty())

	pod := &podList.Items[0]

	// Return the pod name
	return deployment, pod
}

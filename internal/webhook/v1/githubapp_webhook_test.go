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

package v1

import (
	"fmt"
	v2 "github-app-operator/api/v1"
	"os"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// github app private key secret
	privateKeySecret = "gh-app-key-test"
)

var (
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

var _ = Describe("GithubApp Webhook", func() {
	var (
		obj                   *v2.GithubApp
		validator             GithubAppCustomValidator
		rolloutDeploymentSpec *v2.RolloutDeploymentSpec
		vaultPrivateKeySpec   *v2.VaultPrivateKeySpec
		gcpPrivateKeySecret   string
	)
	BeforeEach(func() {
		obj = &v2.GithubApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gh-app-webhook-test",
				Namespace: "default",
			},
			Spec: v2.GithubAppSpec{
				AppId:               appId,
				InstallId:           installId,
				PrivateKeySecret:    privateKeySecret,
				RolloutDeployment:   rolloutDeploymentSpec,
				VaultPrivateKey:     vaultPrivateKeySpec,
				AccessTokenSecret:   acessTokenSecretName,
				GcpPrivateKeySecret: gcpPrivateKeySecret,
			},
		}

		validator = GithubAppCustomValidator{}

		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating GithubApp under Validating Webhook", func() {
		It("Should deny creation if more than one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey is specified", func() {
			obj.Spec.GcpPrivateKeySecret = "this-should-fail"
			Expect(validator.ValidateCreate(ctx, obj)).Error().To(
				MatchError(ContainSubstring("exactly one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey must be specified")),
				"Private key source validation to fail for more than one option")
		})
	})

})

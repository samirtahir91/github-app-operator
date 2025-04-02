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
	"context"
	"fmt"
	githubappv1 "github-app-operator/api/v1"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var githubapplog = logf.Log.WithName("githubapp-resource")

// SetupGithubAppWebhookWithManager will set up the manager to manage the webhooks
func SetupGithubAppWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&githubappv1.GithubApp{}).
		WithValidator(&GithubAppCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-githubapp-samir-io-v1-githubapp,mutating=false,failurePolicy=fail,sideEffects=None,groups=githubapp.samir.io,resources=githubapps,verbs=create;update,versions=v1,name=vgithubapp.kb.io,admissionReviewVersions=v1

type GithubAppCustomValidator struct {
}

var _ webhook.CustomValidator = &GithubAppCustomValidator{}

// ValidateCreate implements webhook.CustomValidator  so a webhook will be registered for the type
func (r *GithubAppCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {

	ghApp, ok := obj.(*githubappv1.GithubApp)
	if !ok {
		return nil, fmt.Errorf("expected a GithubApp object but got %T", obj)
	}
	githubapplog.Info("validate create", "name", ghApp.GetName())

	// Ensure only one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey is specified
	err := validateGithubAppSpec(ghApp)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator  so a webhook will be registered for the type
func (r *GithubAppCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {

	// Ensure only one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey is specified
	ghApp, ok := newObj.(*githubappv1.GithubApp)
	if !ok {
		return nil, fmt.Errorf("expected a GithubApp object but got %T", newObj)
	}
	githubapplog.Info("validate update", "name", ghApp.GetName())

	err := validateGithubAppSpec(ghApp)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator  so a webhook will be registered for the type
func (r *GithubAppCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {

	ghApp, ok := obj.(*githubappv1.GithubApp)
	if !ok {
		return nil, fmt.Errorf("expected a GithubApp object but got %T", obj)
	}

	githubapplog.Info("Validation for GithubApp upon deletion", "name", ghApp.GetName())

	return nil, nil
}

// validateGithubAppSpec validates that only one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey is specified
func validateGithubAppSpec(r *githubappv1.GithubApp) error {
	count := 0

	if r.Spec.GcpPrivateKeySecret != "" {
		count++
	}
	if r.Spec.PrivateKeySecret != "" {
		count++
	}
	if r.Spec.VaultPrivateKey != nil {
		count++
	}

	if count != 1 {
		return fmt.Errorf("exactly one of googlePrivateKeySecret, privateKeySecret, or vaultPrivateKey must be specified")
	}

	return nil
}

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

package v1alpha1

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var rocketaihublog = logf.Log.WithName("rocketaihub-resource")

// +kubebuilder:object:generate=false

type RocketAIHubValidator struct {
	client.Client
}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *RocketAIHub) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(&RocketAIHubValidator{
			mgr.GetClient(),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-ibm-com-v1alpha1-rocketaihub,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.ibm.com,resources=rocketaihubs,verbs=create;update,versions=v1alpha1,name=vrocketaihub.kb.io,admissionReviewVersions=v1

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *RocketAIHubValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	rocketaihublog.Info("Validate the creation of Rocket AI Hub instance")

	// Don't allow to create more than one RocketAIHub instance in the cluster
	// Get the existing RocketAIHub instance in the cluster
	oldObjs := &RocketAIHubList{}
	if err := v.List(ctx, oldObjs); err != nil {
		return nil, fmt.Errorf("failed to get the existing RocketAIHub instances")
	}

	if len(oldObjs.Items) >= 1 {
		return nil, fmt.Errorf("cannot create a new RocketAIHub instance because one already exists in the cluster")
	}

	// Check if the specified identity provider exists in the cluster
	return v.checkIdentityProvider(ctx, obj)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *RocketAIHubValidator) ValidateUpdate(ctx context.Context, oldObj runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	// Check if the specified identity provider exists in the cluster
	return v.checkIdentityProvider(ctx, newObj)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *RocketAIHubValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (r *RocketAIHubValidator) identityProviderExists(ctx context.Context, targetIDP string) (bool, error) {
	// Get the identity providers in the cluster
	oAuth := &configv1.OAuth{}
	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, oAuth); err != nil {
		return false, err
	}
	identityProviders := oAuth.Spec.IdentityProviders
	for _, idp := range identityProviders {
		if targetIDP == idp.Name {
			return true, nil
		}
	}

	return false, nil
}

// Check if the specified identity provider exists in the cluster
func (v *RocketAIHubValidator) checkIdentityProvider(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	rocketaihub := obj.(*RocketAIHub)
	targetIDP := rocketaihub.Spec.IdentityProvider.ExistingIdentityProvider
	if targetIDP != "" {
		exists, err := v.identityProviderExists(ctx, targetIDP)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("cannot find the specified identity provider in cluster. Fix it by either specifying the name of an existing identity provider in the cluster or leaving it empty to use Keycloak as the default identity provider")
		}
	}

	return nil, nil
}

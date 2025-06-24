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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
)

// RocketAIHubReconciler reconciles a RocketAIHub object
type RocketAIHubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const finalizer = "rocketaihub.operator.ibm.com/finalizer"

//+kubebuilder:rbac:groups=operator.ibm.com,resources=rocketaihubs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.ibm.com,resources=rocketaihubs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.ibm.com,resources=rocketaihubs/finalizers,verbs=update
//+kubebuilder:rbac:groups="apps",resources=deployments,verbs=*
//+kubebuilder:rbac:groups="project.openshift.io",resources=projects,verbs=*
//+kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=*
//+kubebuilder:rbac:groups="",resources=pods,verbs=*
//+kubebuilder:rbac:groups="",resources=secrets,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=services,verbs=*
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=*
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=*
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=*
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=*
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=*
//+kubebuilder:rbac:groups="operators.coreos.com",resources=subscriptions,verbs=*
//+kubebuilder:rbac:groups="operators.coreos.com",resources=operatorgroups,verbs=*
//+kubebuilder:rbac:groups="operators.coreos.com",resources=installplans,verbs=*
//+kubebuilder:rbac:groups="nfd.openshift.io",resources=nodefeaturediscoveries,verbs=*
//+kubebuilder:rbac:groups="maistra.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="config.openshift.io",resources=ingresses,verbs=*
//+kubebuilder:rbac:groups="config.openshift.io",resources=oauths,verbs=*
//+kubebuilder:rbac:groups="route.openshift.io",resources=routes,verbs=*
//+kubebuilder:rbac:groups="route.openshift.io",resources=routes/custom-host,verbs=*
//+kubebuilder:rbac:groups="networking.istio.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=*
//+kubebuilder:rbac:groups="apps",resources=statefulsets,verbs=*
//+kubebuilder:rbac:groups="autoscaling",resources=horizontalpodautoscalers,verbs=*
//+kubebuilder:rbac:groups="cert-manager.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="integreatly.org",resources=*,verbs=*
//+kubebuilder:rbac:groups="kubeflow.org",resources=*,verbs=*
//+kubebuilder:rbac:groups="metacontroller.k8s.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=*
//+kubebuilder:rbac:groups="operator.knative.dev",resources=*,verbs=*
//+kubebuilder:rbac:groups="redhatcop.redhat.io",resources=namespaceconfigs,verbs=*
//+kubebuilder:rbac:groups="redhatcop.redhat.io",resources=userconfigs,verbs=*
//+kubebuilder:rbac:groups="security.istio.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="scheduling.k8s.io",resources=priorityclasses,verbs=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
//+kubebuilder:rbac:groups="serving.kserve.io",resources=*,verbs=*
//+kubebuilder:rbac:groups="nvidia.com",resources=*,verbs=*
//+kubebuilder:rbac:groups="security.openshift.io",resources=securitycontextconstraints,verbs=*
//+kubebuilder:rbac:groups=keycloak.org,resources=keycloaks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=keycloak.org,resources=keycloakrealms,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=user.openshift.io,resources=users,verbs=get;list;delete
//+kubebuilder:rbac:groups=user.openshift.io,resources=identities,verbs=get;list;delete
//+kubebuilder:rbac:groups=kubeflow.org,resources=profiles,verbs=get;list;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state for RocketAIHub instance
func (r *RocketAIHubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx).WithName("RocketAIHub Controller")
	log.Info("Reconciling RocketAIHub instance.")

	// Get the RocketAIHub instance
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		if errors.IsNotFound(err) {
			log.Info("RocketAIHub instance not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object, requeue the request
		log.Error(err, "Failed to get RocketAIHub instance")
		return ctrl.Result{}, err
	}

	if rocketaihub.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add the finalizer for resources cleanup before deleting the CR
		if !controllerutil.ContainsFinalizer(rocketaihub, finalizer) {
			controllerutil.AddFinalizer(rocketaihub, finalizer)
			if err := r.Update(ctx, rocketaihub); err != nil {
				return ctrl.Result{}, err
			}
		}

		if err := r.Install(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if controllerutil.ContainsFinalizer(rocketaihub, finalizer) {
			// Cleanup the resources
			if err := r.Uninstall(ctx, req); err != nil {
				return ctrl.Result{}, err
			}

			// Remove the finalizer after the resources cleanup
			controllerutil.RemoveFinalizer(rocketaihub, finalizer)
			if err := r.Update(ctx, rocketaihub); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RocketAIHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// With GenerationChangedPredicate, any update events with writes only to the status field will not be reconciled.
	return ctrl.NewControllerManagedBy(mgr).
		For(&rocketaihubv1alpha1.RocketAIHub{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

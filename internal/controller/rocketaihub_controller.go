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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logr "sigs.k8s.io/controller-runtime/pkg/log"

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
		// Error reading the object, requeque the request.
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
	} else {
		if controllerutil.ContainsFinalizer(rocketaihub, finalizer) {
			// Cleanup the resources
			if err := r.Uninstall(ctx, r.Client); err != nil {
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

	if err := r.Install(ctx, r.Client, rocketaihub); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RocketAIHubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rocketaihubv1alpha1.RocketAIHub{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

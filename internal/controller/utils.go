package controller

import (
	"context"
	"os"
	"path/filepath"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
	"github.com/IBM/rocketaihub-operator/pkg/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ManifestRootPath = os.Getenv("MANIFEST_ROOT_PATH")
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, req ctrl.Request) error {
	log := logr.FromContext(ctx).WithName("RocketAIHub Controller")

	manifestPath := filepath.Join(ManifestRootPath, "servicemesh")
	if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	// Update status of the CR
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		// Error reading the object, requeque the request.
		log.Error(err, "Failed to get RocketAIHub instance")
		return err
	}

	condition := metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "reason",
		Message:            "message",
	}

	if !contains(rocketaihub.Status.Conditions, condition) {
		rocketaihub.Status.Conditions = append(rocketaihub.Status.Conditions, condition)
		if err := r.Status().Update(ctx, rocketaihub); err != nil {
			log.Error(err, "Resource status update failed.")
		}
	}

	return nil
}

// Uninstall the components
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context) error {
	manifestPath := filepath.Join(ManifestRootPath, "servicemesh")
	return resources.DeleteResources(ctx, r.Client, manifestPath)
}

// Check if the condition already exists in the conditions of the CR
func contains(conditions []metav1.Condition, condition metav1.Condition) bool {
	for _, e := range conditions {
		if e.Type == condition.Type && e.Status == condition.Status && e.Reason == condition.Reason && e.Message == condition.Message {
			return true
		}
	}

	return false
}

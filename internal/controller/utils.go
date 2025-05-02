package controller

import (
	"context"
	"os"
	"path/filepath"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
	"github.com/IBM/rocketaihub-operator/pkg/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ManifestRootPath = os.Getenv("MANIFEST_ROOT_PATH")
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, client client.Client, obj *rocketaihubv1alpha1.RocketAIHub) error {
	log := logr.FromContext(ctx).WithName("RocketAIHub Controller")

	manifestPath := filepath.Join(ManifestRootPath, "deployment")
	if err := resources.CreateResources(ctx, client, manifestPath); err != nil {
		return err
	}

	// Update status of the CR
	condition := metav1.Condition{
		Type:               "Progressing",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "reason",
		Message:            "message",
	}

	if !contains(obj.Status.Conditions, condition) {
		obj.Status.Conditions = append(obj.Status.Conditions, condition)
		if err := r.Status().Update(ctx, obj); err != nil {
			log.Error(err, "Resource status update failed.")
		}
	}

	return nil
}

// Uninstall the components
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context, client client.Client) error {
	manifestPath := filepath.Join(ManifestRootPath, "deployment")
	return resources.DeleteResources(ctx, client, manifestPath)
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

package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
	"github.com/IBM/rocketaihub-operator/pkg/resources"
	helmclient "github.com/mittwald/go-helm-client"
	"github.com/mittwald/go-helm-client/values"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	manifestRootPath   = os.Getenv("MANIFEST_ROOT_PATH")
	certManagerVersion = os.Getenv("CERT_MANAGER_VERSION")
	loggerName         = "RocketAIHub Controller"
	valueOptions       = values.Options{Values: []string{"installCRDs=true"}}
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, req ctrl.Request) error {
	// Install Cert Manager operator using helm, then update the satus of CR
	operatorName := "Cert Manager Operator"
	releaseName := "cert-manager"
	repoName := "jetstack"
	repoURL := "https://charts.jetstack.io"
	// Variables for CR status condition
	conditionType := "CertManagerIsReady"
	conditionStatus := metav1.ConditionTrue
	conditionReason := "InstallSuccessful"
	conditionMessage := fmt.Sprintf("Installation of %s %s is successful", operatorName, certManagerVersion)
	err := installHelmChart(ctx, operatorName, repoName, repoURL, releaseName, certManagerVersion, valueOptions)
	if err != nil {
		conditionStatus = metav1.ConditionFalse
		conditionReason = "InstallUnsuccessful"
		conditionMessage = err.Error()
		if err := r.updateStatus(ctx, req, conditionType, conditionStatus, conditionReason, conditionMessage); err != nil {
			return err
		}

		return err
	} else {
		if err := r.updateStatus(ctx, req, conditionType, conditionStatus, conditionReason, conditionMessage); err != nil {
			return err
		}
	}

	// manifestPath := filepath.Join(manifestRootPath, "servicemesh")
	// if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
	// 	return err
	// }

	return nil
}

func (r *RocketAIHubReconciler) updateStatus(ctx context.Context, req ctrl.Request, condType string, condStatus metav1.ConditionStatus, condReason, condMessage string) error {
	log := logr.FromContext(ctx).WithName(loggerName)
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		// Error reading the object, requeque the request.
		log.Error(err, "Failed to get RocketAIHub instance")
		return err
	}

	condition := metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		LastTransitionTime: metav1.Now(),
		Reason:             condReason,
		Message:            condMessage,
	}

	if !contains(rocketaihub.Status.Conditions, condition) {
		rocketaihub.Status.Conditions = append(rocketaihub.Status.Conditions, condition)
		if err := r.Status().Update(ctx, rocketaihub); err != nil {
			log.Error(err, "Resource status update failed.")
		}
	}

	return nil
}

func installHelmChart(ctx context.Context, operatorName string, repoName string, repoURL string, releaseName string, version string, valueOptions values.Options) error {
	log := logr.FromContext(ctx).WithName(loggerName)
	log.Info(fmt.Sprintf("Starting the installation of %s %s", operatorName, version))

	chartName := repoName + "/" + releaseName
	opt := &helmclient.Options{
		Namespace:        releaseName,
		RepositoryCache:  "/tmp/.helmcache",
		RepositoryConfig: "/tmp/.helmrepo",
		Linting:          true,
	}
	helmClient, err := helmclient.New(opt)
	if err != nil {
		return err
	}

	// Check if the chart is already installed
	release, err := helmClient.GetRelease(releaseName)
	if err != nil && errors.IsNotFound(err) {
		log.Error(err, "Failed to get release "+releaseName)
		return err
	} else if release == nil || release.Chart.Metadata.Version != version {
		// Add the helm repo
		chartRepo := repo.Entry{
			Name: repoName,
			URL:  repoURL,
		}
		if err := helmClient.AddOrUpdateChartRepo(chartRepo); err != nil {
			log.Error(err, "Failed to add or update char repo with URL "+repoURL)
			return err
		}
		// Install the helm chart
		chartSpec := helmclient.ChartSpec{
			ReleaseName:     releaseName,
			ChartName:       chartName,
			Namespace:       releaseName,
			Version:         version,
			CreateNamespace: true,
			SkipCRDs:        false,
			UpgradeCRDs:     true,
			ValuesOptions:   valueOptions,
		}
		if _, err := helmClient.InstallOrUpgradeChart(ctx, &chartSpec, nil); err != nil {
			log.Error(err, fmt.Sprintf("Failed to install %s %s ", operatorName, version))
			return err
		}
	}

	return nil
}

// Uninstall the components
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context) error {
	manifestPath := filepath.Join(manifestRootPath, "servicemesh")
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

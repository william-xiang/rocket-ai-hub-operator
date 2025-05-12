package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
	"github.com/IBM/rocketaihub-operator/pkg/resources"
	helmclient "github.com/mittwald/go-helm-client"
	"github.com/mittwald/go-helm-client/values"
	"helm.sh/helm/v3/pkg/repo"

	servingv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	manifestRootPath           = os.Getenv("MANIFEST_ROOT_PATH")
	certManagerVersion         = os.Getenv("CERT_MANAGER_VERSION")
	valueOptions               = values.Options{Values: []string{"installCRDs=true"}}
	certManagerIsReady         = "CertManagerIsReady"
	dependentOperatorsAreReady = "DependentOperatorsAreReady"
	servicemeshIsReady         = "ServiceMeshIsReady"
	kubeflowIsReady            = "KubeflowIsReady"
	installSuccessful          = "InstallSuccessful"
	installUnuccessful         = "InstallUnsuccessful"
	logger                     = logr.Log.WithName("RocketAIHub Controller")
	retryInterval              = 30 * time.Second
	timeout                    = 600 * time.Second
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, req ctrl.Request) error {
	// Install Cert Manager operator using helm, then update the satus of CR
	operatorName := "Cert Manager Operator"
	releaseName := "cert-manager"
	repoName := "jetstack"
	repoURL := "https://charts.jetstack.io"
	conditionMessage := fmt.Sprintf("Installation of %s %s is successful", operatorName, certManagerVersion)
	err := installHelmChart(ctx, operatorName, repoName, repoURL, releaseName, certManagerVersion, valueOptions)
	if err != nil {
		conditionMessage = err.Error()
		if err := r.updateStatus(ctx, req, certManagerIsReady, metav1.ConditionFalse, installUnuccessful, conditionMessage); err != nil {
			return err
		}

		return err
	} else {
		if err := r.updateStatus(ctx, req, certManagerIsReady, metav1.ConditionTrue, installSuccessful, conditionMessage); err != nil {
			return err
		}
	}

	// Install operators Service Mesh (incl. Elasticsearch, Kiali, Jaeger), Namespace-Configuration, Serverless, Node Feature Discovery, GPU Operator, and Grafana
	manifestPath := filepath.Join(manifestRootPath, "subscriptions")
	conditionMessage = "Installation of dependent operators is successful"
	err = resources.CreateResources(ctx, r.Client, manifestPath)
	if err != nil {
		conditionMessage = err.Error()
		if err := r.updateStatus(ctx, req, dependentOperatorsAreReady, metav1.ConditionFalse, installUnuccessful, conditionMessage); err != nil {
			return err
		}

		return err
	} else {
		if err := r.updateStatus(ctx, req, dependentOperatorsAreReady, metav1.ConditionTrue, installSuccessful, conditionMessage); err != nil {
			return err
		}
	}

	// Configure node feature discovery
	// Wait for the CRD NodeFeatureDiscovery is installed
	namespacedName := types.NamespacedName{Name: "nodefeaturediscoveries.nfd.openshift.io"}
	err = r.waitForObject(ctx, &apiextv1.CustomResourceDefinition{}, namespacedName, retryInterval, timeout)
	if err != nil {
		return err
	}
	manifestPath = filepath.Join(manifestRootPath, "nfd")
	if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	// Configure grafana
	manifestPath = filepath.Join(manifestRootPath, "grafana")
	if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	// Configure service mesh
	// Wait for deployment istio-operator is ready
	namespacedName = types.NamespacedName{
		Name:      "istio-operator",
		Namespace: "openshift-operators",
	}
	err = r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	if err != nil {
		return err
	}
	manifestPath = filepath.Join(manifestRootPath, "servicemesh")
	if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}
	// Wait for deployment istiod-kubeflow is ready
	namespacedName = types.NamespacedName{
		Name:      "istiod-kubeflow",
		Namespace: "istio-system",
	}
	err = r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	conditionMessage = "Service mesh is configured successfully"
	if err != nil {
		conditionMessage = err.Error()
		if err := r.updateStatus(ctx, req, servicemeshIsReady, metav1.ConditionFalse, installUnuccessful, conditionMessage); err != nil {
			return err
		}

		return err
	} else {
		if err := r.updateStatus(ctx, req, servicemeshIsReady, metav1.ConditionTrue, installSuccessful, conditionMessage); err != nil {
			return err
		}
	}

	// Deploy Kubeflow
	if err := resources.CreateResources(ctx, r.Client, manifestRootPath); err != nil {
		return err
	}
	// Wait for deployment centraldashboard is ready
	namespacedName = types.NamespacedName{
		Name:      "centraldashboard",
		Namespace: "kubeflow",
	}
	err = r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	conditionMessage = "Installation of Kubeflow is successful"
	if err != nil {
		conditionMessage = err.Error()
		if err := r.updateStatus(ctx, req, kubeflowIsReady, metav1.ConditionFalse, installUnuccessful, conditionMessage); err != nil {
			return err
		}

		return err
	} else {
		if err := r.updateStatus(ctx, req, kubeflowIsReady, metav1.ConditionTrue, installSuccessful, conditionMessage); err != nil {
			return err
		}
	}

	logger.Info("Instance of Rocket AI Hub operator is deployed successfully!")
	return nil
}

func (r *RocketAIHubReconciler) updateStatus(ctx context.Context, req ctrl.Request, condType string, condStatus metav1.ConditionStatus, condReason, condMessage string) error {
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		// Error reading the object, requeque the request.
		logger.Error(err, "Failed to get RocketAIHub instance")
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
			logger.Error(err, "Resource status update failed.")
		}
	}

	return nil
}

func installHelmChart(ctx context.Context, operatorName string, repoName string, repoURL string, releaseName string, version string, valueOptions values.Options) error {
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
		logger.Error(err, "Failed to get release "+releaseName)
		return err
	} else if release == nil || release.Chart.Metadata.Version != version {
		logger.Info(fmt.Sprintf("Starting the installation of %s %s", operatorName, version))
		// Add the helm repo
		chartRepo := repo.Entry{
			Name: repoName,
			URL:  repoURL,
		}
		if err := helmClient.AddOrUpdateChartRepo(chartRepo); err != nil {
			logger.Error(err, "Failed to add or update char repo with URL "+repoURL)
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
			logger.Error(err, fmt.Sprintf("Failed to install %s %s ", operatorName, version))
			return err
		}
	}

	return nil
}

// Uninstall the components
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context) error {
	// Delete all the existing inference servcie instance
	// Ignore the error that InferenceService kind cannot be found
	inferenceServiceList := servingv1beta1.InferenceServiceList{}
	if err := r.Client.List(ctx, &inferenceServiceList); err != nil && !meta.IsNoMatchError(err) {
		return err
	}
	for _, infService := range inferenceServiceList.Items {
		if err := r.Client.Delete(ctx, &infService); err != nil {
			return err
		}
	}

	// Delete validatingwebhookconfiguration validation.webhook.serving.knative.dev
	webhook := admissionregistrationv1.ValidatingWebhookConfiguration{}
	namespacedName := types.NamespacedName{Name: "validation.webhook.serving.knative.dev"}
	if err := r.Client.Get(ctx, namespacedName, &webhook); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	} else {
		if err := r.Client.Delete(ctx, &webhook); err != nil {
			return err
		}
	}

	// Delete the kubeflow
	// Deploy Kubeflow
	if err := resources.DeleteResources(ctx, r.Client, manifestRootPath); err != nil {
		return err
	}
	// Delete the service mesh configuration
	manifestPath := filepath.Join(manifestRootPath, "servicemesh")
	if err := resources.DeleteResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	logger.Info("Instance of Rocket AI Hub operator is removed successfully!")
	return nil
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

func (r *RocketAIHubReconciler) waitForObject(ctx context.Context, obj client.Object, namespacedName types.NamespacedName, retryInterval, timeout time.Duration) error {
	kind := fmt.Sprintf("%T", obj)
	err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, true, func(ctx context.Context) (done bool, err error) {
		if err := r.Client.Get(ctx, namespacedName, obj); err != nil {
			if errors.IsNotFound(err) {
				logger.Info(fmt.Sprintf("Wait for %s %s", kind, namespacedName.Name))
				return false, nil
			}

			logger.Error(err, fmt.Sprintf("Failed to get %s %s", kind, namespacedName.Name))
			return false, err
		}
		// If the object is a Deployment, the number of replicas in status should match the number in spec
		switch v := obj.(type) {
		case *appsv1.Deployment:
			if v.Status.Replicas < *v.Spec.Replicas {
				logger.Info(fmt.Sprintf("Wait for %s %s to be ready", kind, namespacedName.Name))
				return false, nil
			}
		}
		logger.Info(fmt.Sprintf("%s %s is Ready", kind, namespacedName.Name))
		return true, nil
	})
	if err != nil {
		logger.Error(err, fmt.Sprintf("Failed to wait for %s %s", kind, namespacedName.Name))
		return err
	}
	return nil
}

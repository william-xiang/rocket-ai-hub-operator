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
	manifestRootPath           = "/manifests/overlays/openshift"
	gpuOperatorPath            = "/gpu-operator"
	valueOptions               = values.Options{Values: []string{"installCRDs=true"}}
	certManagerIsReady         = "CertManagerIsReady"
	dependentOperatorsAreReady = "DependentOperatorsAreReady"
	servicemeshIsReady         = "ServiceMeshIsReady"
	kubeflowIsReady            = "KubeflowIsReady"
	gpuOperatorIsReady         = "GpuOperatorIsReady"
	installSuccessful          = "InstallSuccessful"
	installUnsuccessful        = "InstallUnsuccessful"
	logger                     = logr.Log.WithName("RocketAIHub Controller")
	retryInterval              = 30 * time.Second
	timeout                    = 600 * time.Second
	certManagerVersion         = "v1.5.4"
	certManagerRepoName        = "jetstack"
	certManagerReleaseName     = "cert-manager"
	gpuOperatorReleaseName     = "gpu-operator"
	gpuOperatorVersion         = "v1.10.1-ubi8"
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, req ctrl.Request) error {
	// Change the path of manifests and GPU operator for debugging
	if rootPath := os.Getenv("PROJECT_ROOT_PATH"); rootPath != "" {
		manifestRootPath = filepath.Join(rootPath, "manifests/overlays/openshift")
		gpuOperatorPath = filepath.Join(rootPath, "gpu-operator/deployments/gpu-operator")
	}

	// Install Cert Manager operator using helm, then update the status of CR
	// Install the helm chart
	chartSpec := helmclient.ChartSpec{
		ReleaseName:     certManagerReleaseName,
		ChartName:       certManagerRepoName + "/" + certManagerReleaseName,
		Namespace:       certManagerReleaseName,
		Version:         certManagerVersion,
		CreateNamespace: true,
		SkipCRDs:        false,
		UpgradeCRDs:     true,
		ValuesOptions:   valueOptions,
	}
	chartRepo := repo.Entry{
		Name: certManagerRepoName,
		URL:  "https://charts.jetstack.io",
	}
	conditionMessage := fmt.Sprintf("Installation of %s %s is successful", certManagerReleaseName, certManagerVersion)
	installErr := installHelmChart(ctx, chartSpec, &chartRepo)
	if err := r.updateStatus(ctx, req, certManagerIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if installErr != nil {
		return installErr
	}

	// Install operators Service Mesh (incl. Elasticsearch, Kiali, Jaeger), Namespace-Configuration, Serverless, Node Feature Discovery, GPU Operator, and Grafana
	manifestPath := filepath.Join(manifestRootPath, "subscriptions")
	conditionMessage = "Installation of dependent operators is successful"
	installErr = resources.CreateResources(ctx, r.Client, manifestPath)
	if err := r.updateStatus(ctx, req, dependentOperatorsAreReady, conditionMessage, installErr); err != nil {
		return err
	}
	if installErr != nil {
		return installErr
	}

	// Configure node feature discovery
	// Wait for the CRD NodeFeatureDiscovery is installed
	namespacedName := types.NamespacedName{Name: "nodefeaturediscoveries.nfd.openshift.io"}
	if err := r.waitForObject(ctx, &apiextv1.CustomResourceDefinition{}, namespacedName, retryInterval, timeout); err != nil {
		return err
	}
	manifestPath = filepath.Join(manifestRootPath, "nfd")
	if err := resources.CreateResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	// Install GPU Operator
	rocketaihub := rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, &rocketaihub); err != nil {
		return err
	}
	if rocketaihub.Spec.Components.GpuOperator {
		chartSpec := helmclient.ChartSpec{
			ReleaseName:     gpuOperatorReleaseName,
			ChartName:       gpuOperatorPath, // chart name is the path of chart for chart from local directory
			Namespace:       gpuOperatorReleaseName,
			Version:         gpuOperatorVersion,
			CreateNamespace: true,
			SkipCRDs:        false,
			UpgradeCRDs:     true,
			ValuesOptions:   valueOptions,
		}
		conditionMessage := fmt.Sprintf("Installation of %s %s is successful", gpuOperatorReleaseName, gpuOperatorVersion)
		installErr := installHelmChart(ctx, chartSpec, nil)
		if err := r.updateStatus(ctx, req, gpuOperatorIsReady, conditionMessage, installErr); err != nil {
			return err
		}
		if installErr != nil {
			return installErr
		}
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
	if err := r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout); err != nil {
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
	conditionMessage = "Service mesh is configured successfully"
	waitErr := r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	if err := r.updateStatus(ctx, req, servicemeshIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if waitErr != nil {
		return waitErr
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
	conditionMessage = "Installation of Kubeflow is successful"
	waitErr = r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	if err := r.updateStatus(ctx, req, kubeflowIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if waitErr != nil {
		return waitErr
	}

	logger.Info("Instance of Rocket AI Hub operator is deployed successfully!")
	return nil
}

func (r *RocketAIHubReconciler) updateStatus(ctx context.Context, req ctrl.Request, condType string, condMessage string, err error) error {
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		// Error reading the object, requeue the request
		logger.Error(err, "Failed to get RocketAIHub instance")
		return err
	}

	condStatus := metav1.ConditionTrue
	condReason := installSuccessful
	if err != nil {
		condStatus = metav1.ConditionFalse
		condReason = installUnsuccessful
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
			return err
		}
	}

	return nil
}

func getHelmClient(nameSpace string) (helmclient.Client, error) {
	opt := &helmclient.Options{
		Namespace:        nameSpace,
		RepositoryCache:  "/tmp/.helmcache",
		RepositoryConfig: "/tmp/.helmrepo",
		Linting:          true,
	}
	helmClient, err := helmclient.New(opt)
	if err != nil {
		return nil, err
	}

	return helmClient, nil
}

// For the installation of chart using local chart directory, local chart archive or url to a chart archive, charRepo should be nil
func installHelmChart(ctx context.Context, chartSpec helmclient.ChartSpec, chartRepo *repo.Entry) error {
	helmClient, err := getHelmClient(chartSpec.ReleaseName)
	if err != nil {
		return err
	}

	// Check if the chart is already installed
	release, err := helmClient.GetRelease(chartSpec.ReleaseName)
	if err != nil && errors.IsNotFound(err) {
		logger.Error(err, "Failed to get release "+chartSpec.ReleaseName)
		return err
	} else if release == nil || release.Chart.Metadata.Version != chartSpec.Version {
		logger.Info(fmt.Sprintf("Starting the installation of %s %s", chartSpec.ReleaseName, chartSpec.Version))
		// If chartRepo is not nil, it means the chart is from a remote chart repo
		// The repo needs to be added locally before it can be installed
		if chartRepo != nil {
			// A chart repository is an HTTP server that provides information on charts
			// After adding the chart repository, a local repository cache will be created for the chart
			if err := helmClient.AddOrUpdateChartRepo(*chartRepo); err != nil {
				logger.Error(err, "Failed to add or update char repo with URL "+chartRepo.URL)
				return err
			}
		}

		if _, err := helmClient.InstallOrUpgradeChart(ctx, &chartSpec, nil); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to install %s %s ", chartSpec.ReleaseName, chartSpec.Version))
			return err
		}
	} else {
		logger.Info(fmt.Sprintf("%s %s is already installed in the cluster", chartSpec.ReleaseName, chartSpec.Version))
	}

	return nil
}

// Uninstall the components
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context) error {
	// Delete all the existing inference service instance
	// List function implicitly triggers a watch via the underlying informer cache, which will cause some issue after deleting the CRD InferenceService
	// So change to use an uncached client to send the one-time request to the server
	uncachedClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: r.Scheme})
	if err != nil {
		return err
	}
	inferenceServiceList := servingv1beta1.InferenceServiceList{}
	// Ignore the error that InferenceService kind cannot be found
	if err := uncachedClient.List(ctx, &inferenceServiceList); err != nil && !meta.IsNoMatchError(err) {
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
	if err := resources.DeleteResources(ctx, r.Client, manifestRootPath); err != nil {
		return err
	}

	// Delete the service mesh configuration
	manifestPath := filepath.Join(manifestRootPath, "servicemesh")
	if err := resources.DeleteResources(ctx, r.Client, manifestPath); err != nil {
		return err
	}

	// Uninstall the GPU operator
	helmClient, err := getHelmClient(gpuOperatorReleaseName)
	if err != nil {
		return err
	}
	if err := helmClient.UninstallReleaseByName(gpuOperatorReleaseName); err != nil && !errors.IsNotFound(err) {
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

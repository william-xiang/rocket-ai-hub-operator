package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	rocketaihubv1alpha1 "github.com/IBM/rocketaihub-operator/api/v1alpha1"
	"github.com/IBM/rocketaihub-operator/pkg/resources"
	version "github.com/IBM/rocketaihub-operator/version"
	helmclient "github.com/mittwald/go-helm-client"
	"github.com/mittwald/go-helm-client/values"
	"helm.sh/helm/v3/pkg/repo"

	servingv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	profilev1beta1 "github.com/kubeflow/kubeflow/components/profile-controller/api/v1beta1"
	configv1 "github.com/openshift/api/config/v1"
	projectv1 "github.com/openshift/api/project/v1"
	userv1 "github.com/openshift/api/user/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	redhatcop "github.com/redhat-cop/namespace-configuration-operator/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	keycloakPath               = "/keycloak"
	valueOptions               = values.Options{Values: []string{"installCRDs=true"}}
	certManagerIsReady         = "CertManagerIsReady"
	dependentOperatorsAreReady = "DependentOperatorsAreReady"
	servicemeshIsReady         = "ServiceMeshIsReady"
	kubeflowIsReady            = "KubeflowIsReady"
	gpuOperatorIsReady         = "GpuOperatorIsReady"
	installSuccessful          = "InstallSuccessful"
	installUnsuccessful        = "InstallUnsuccessful"
	logger                     = logr.Log.WithName("RocketAIHub Controller")
	retryInterval              = 10 * time.Second
	timeout                    = 300 * time.Second
	certManagerVersion         = "v1.5.4"
	certManagerRepoName        = "jetstack"
	certManagerReleaseName     = "cert-manager"
	gpuOperatorReleaseName     = "gpu-operator"
	gpuOperatorVersion         = "v1.10.1-ubi8"
	keycloakProjectName        = "rocketaihub-keycloak"
	keycloakSubscription       = "keycloak-subscription"
	keycloakPackageName        = "rhsso-operator"
	rocketaihubIDPName         = "rocketaihub"
	idpClientSecretName        = "rocketaihub-client-secret"
	idpCAConfigMapName         = "rocketaihub-ca-configmap"
)

// Install all the components
func (r *RocketAIHubReconciler) Install(ctx context.Context, req ctrl.Request) error {
	// Change the path of manifests and GPU operator for debugging
	rootPath := os.Getenv("PROJECT_ROOT_PATH")
	if rootPath != "" {
		manifestRootPath = filepath.Join(rootPath, "kubeflow-ppc64le-manifests/overlays/openshift")
		gpuOperatorPath = filepath.Join(rootPath, "gpu-operator/deployments/gpu-operator")
		keycloakPath = filepath.Join(rootPath, "keycloak")
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
	if err := r.updateCondition(ctx, req, certManagerIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if installErr != nil {
		return installErr
	}

	// Install operators Service Mesh (incl. Elasticsearch, Kiali, Jaeger), Namespace-Configuration, Serverless, Node Feature Discovery, GPU Operator, and Grafana
	manifestPath := filepath.Join(manifestRootPath, "subscriptions")
	conditionMessage = "Installation of dependent operators is successful"
	installErr = resources.CreateResources(ctx, r.Client, manifestPath, nil)
	if err := r.updateCondition(ctx, req, dependentOperatorsAreReady, conditionMessage, installErr); err != nil {
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
	if err := resources.CreateResources(ctx, r.Client, manifestPath, nil); err != nil {
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
		if err := r.updateCondition(ctx, req, gpuOperatorIsReady, conditionMessage, installErr); err != nil {
			return err
		}
		if installErr != nil {
			return installErr
		}
	}

	// Configure grafana
	manifestPath = filepath.Join(manifestRootPath, "grafana")
	if err := resources.CreateResources(ctx, r.Client, manifestPath, nil); err != nil {
		return err
	}

	// Approve all the pending install plans
	if err := r.approveInstallPlans(ctx); err != nil {
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
	// Create a new environment variable for cluster domain
	clusterDomain, err := setClusterDomain(ctx, r.Client)
	if err != nil {
		return err
	}
	manifestPath = filepath.Join(manifestRootPath, "servicemesh")
	filePath := filepath.Join(manifestPath, "global-params.env")
	if err := resources.CreateResources(ctx, r.Client, manifestPath, []string{filePath}); err != nil {
		return err
	}
	// Wait for deployment istiod-kubeflow is ready
	namespacedName = types.NamespacedName{
		Name:      "istiod-kubeflow",
		Namespace: "istio-system",
	}
	conditionMessage = "Service mesh is configured successfully"
	waitErr := r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	if err := r.updateCondition(ctx, req, servicemeshIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if waitErr != nil {
		return waitErr
	}

	// Deploy Kubeflow
	if err := resources.CreateResources(ctx, r.Client, manifestRootPath, nil); err != nil {
		return err
	}
	// Wait for deployment centraldashboard is ready
	namespacedName = types.NamespacedName{
		Name:      "centraldashboard",
		Namespace: "kubeflow",
	}
	conditionMessage = "Installation of Kubeflow is successful"
	waitErr = r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout)
	if err := r.updateCondition(ctx, req, kubeflowIsReady, conditionMessage, installErr); err != nil {
		return err
	}
	if waitErr != nil {
		return waitErr
	}

	// Configure UserConfig object
	if err := r.configureOAuth(ctx, req); err != nil {
		return err
	}

	// Update status of RocketAIHub CR
	status := rocketaihub.Status
	status.KeycloakURL = "https://kubeflow." + clusterDomain
	status.KubeflowURL = "https://keycloak-rocketaihub-keycloak." + clusterDomain
	status.KubeflowVersion = version.KubeflowVersion
	if err := r.updateStatus(ctx, req, status); err != nil {
		return err
	}

	logger.Info("Instance of Rocket AI Hub operator is deployed successfully!")
	return nil
}

func (r *RocketAIHubReconciler) updateCondition(ctx context.Context, req ctrl.Request, condType string, condMessage string, err error) error {
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

	if !containsCondition(rocketaihub.Status.Conditions, condition) {
		rocketaihub.Status.Conditions = append(rocketaihub.Status.Conditions, condition)
		if err := r.Status().Update(ctx, rocketaihub); err != nil {
			logger.Error(err, "Resource status update failed.")
			return err
		}
	}

	return nil
}

func (r *RocketAIHubReconciler) updateStatus(ctx context.Context, req ctrl.Request, newStatus rocketaihubv1alpha1.RocketAIHubStatus) error {
	rocketaihub := &rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, rocketaihub); err != nil {
		// Error reading the object, requeue the request
		logger.Error(err, "Failed to get RocketAIHub instance")
		return err
	}

	rocketaihub.Status.KeycloakURL = newStatus.KeycloakURL
	rocketaihub.Status.KubeflowURL = newStatus.KubeflowURL
	rocketaihub.Status.KubeflowVersion = version.KubeflowVersion
	if err := r.Status().Update(ctx, rocketaihub); err != nil {
		logger.Error(err, "Resource status update failed.")
		return err
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
func (r *RocketAIHubReconciler) Uninstall(ctx context.Context, req ctrl.Request) error {
	// Change the path of manifests and GPU operator for debugging
	rootPath := os.Getenv("PROJECT_ROOT_PATH")
	if rootPath != "" {
		manifestRootPath = filepath.Join(rootPath, "kubeflow-ppc64le-manifests/overlays/openshift")
		gpuOperatorPath = filepath.Join(rootPath, "gpu-operator/deployments/gpu-operator")
		keycloakPath = filepath.Join(rootPath, "keycloak")
	}

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
	if err := resources.DeleteResources(ctx, r.Client, manifestRootPath, nil); err != nil {
		return err
	}

	// Delete the service mesh configuration
	manifestPath := filepath.Join(manifestRootPath, "servicemesh")
	if err := resources.DeleteResources(ctx, r.Client, manifestPath, nil); err != nil {
		return err
	}

	// Uninstall the GPU operator
	helmClient, err := getHelmClient(gpuOperatorReleaseName)
	if err != nil {
		return err
	}
	err = helmClient.UninstallReleaseByName(gpuOperatorReleaseName)
	if err != nil && !strings.HasPrefix(err.Error(), "uninstall: Release not loaded") {
		return err
	}

	// Delete the Keycloak realm
	rocketaihub := rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, &rocketaihub); err != nil {
		return err
	}
	idpName := rocketaihub.Spec.IdentityProvider.ExistingIdentityProvider
	if idpName == "" {
		var keycloakRealmPath string
		if rocketaihub.Spec.IdentityProvider.CreateDefaultUser {
			keycloakRealmPath = filepath.Join(keycloakPath, "keycloak-realm-with-user")
		} else {
			keycloakRealmPath = filepath.Join(keycloakPath, "keycloak-realm-without-user")
		}
		if err := resources.DeleteResources(ctx, r.Client, keycloakRealmPath, nil); err != nil {
			return err
		}
	}

	// Remove the Keycloak identity provider from OpenShift OAuth
	if rocketaihub.Spec.IdentityProvider.ExistingIdentityProvider == "" {
		oAuth := &configv1.OAuth{}
		if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, oAuth); err != nil {
			return err
		}
		newIDPs := []configv1.IdentityProvider{}
		for _, idp := range oAuth.Spec.IdentityProviders {
			if idp.Name != rocketaihubIDPName {
				newIDPs = append(newIDPs, idp)
			}
		}
		if len(newIDPs) < len(oAuth.Spec.IdentityProviders) {
			oAuth.Spec.IdentityProviders = newIDPs
			if err := r.Update(ctx, oAuth); err != nil {
				return err
			}
		}
	}

	// Delete all the profiles
	profileList := profilev1beta1.ProfileList{}
	if err := uncachedClient.List(ctx, &profileList); err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}
	for _, profile := range profileList.Items {
		if err := r.Client.Delete(ctx, &profile); err != nil {
			return err
		}
	}

	// Delete all the users from the Keycloak identity provider
	userList := userv1.UserList{}
	if err := uncachedClient.List(ctx, &userList); err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}
	for _, user := range userList.Items {
		identities := user.Identities
		if len(identities) == 1 && strings.HasPrefix(identities[0], rocketaihubIDPName) {
			if err := r.Client.Delete(ctx, &user); err != nil {
				return err
			}
		}
	}

	// Delete all the identities associated with the Keycloak identity provider
	identityList := userv1.IdentityList{}
	if err := uncachedClient.List(ctx, &identityList); err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}
	for _, identity := range identityList.Items {
		if strings.HasPrefix(identity.Name, rocketaihubIDPName) {
			if err := r.Client.Delete(ctx, &identity); err != nil {
				return err
			}
		}
	}

	logger.Info("Instance of Rocket AI Hub operator is removed successfully!")
	return nil
}

// Check if the condition already exists in the conditions of the CR
func containsCondition(conditions []metav1.Condition, condition metav1.Condition) bool {
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

func startsWithString(strs []string, str string) bool {
	for _, v := range strs {
		if strings.HasPrefix(str, v) {
			return true
		}
	}
	return false
}

// Approve all the pending install plans
func (r *RocketAIHubReconciler) approveInstallPlans(ctx context.Context) error {
	installPlans := operatorsv1alpha1.InstallPlanList{}
	if err := r.List(ctx, &installPlans); err != nil {
		return err
	}

	for _, installPlan := range installPlans.Items {
		expectedCSVNames := []string{
			"elasticsearch-operator",
			"kiali-operator",
			"serverless-operator",
			"servicemeshoperator",
			"jaeger-operator",
			"grafana-operator",
			"namespace-configuration-operator",
			"nfd",
		}
		phase := installPlan.Status.Phase
		csvName := installPlan.Spec.ClusterServiceVersionNames[0]
		// The install plan needs to be approved which requires manual approval and CSV name is the expected one
		if phase == operatorsv1alpha1.InstallPlanPhaseRequiresApproval && startsWithString(expectedCSVNames, csvName) {
			logger.Info(fmt.Sprintf("Approving the install plan %s in project %s", installPlan.Name, installPlan.Namespace))
			installPlan.Spec.Approved = true
			if err := r.Update(ctx, &installPlan); err != nil {
				return err
			}
		}
	}
	return nil
}

func setClientSecret() (string, error) {
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	clientSecret := base64.StdEncoding.EncodeToString(randomBytes)
	os.Setenv("CLIENT_SECRET", clientSecret)
	return clientSecret, nil
}

func setClusterDomain(ctx context.Context, client client.Client) (string, error) {
	ingress := &configv1.Ingress{}
	err := client.Get(ctx, types.NamespacedName{Name: "cluster"}, ingress)
	if err != nil {
		return "", err
	}
	clusterDomain := ingress.Spec.Domain
	os.Setenv("CLUSTER_DOMAIN", clusterDomain)
	return clusterDomain, nil
}

// This method will install Keycloak and add it in OAuth as identity provider if no existing identity provider is specified during the installation.
func (r *RocketAIHubReconciler) configureOAuth(ctx context.Context, req ctrl.Request) error {
	rocketaihub := rocketaihubv1alpha1.RocketAIHub{}
	if err := r.Get(ctx, req.NamespacedName, &rocketaihub); err != nil {
		return err
	}
	// Install and configure Keycloak if the existing identity provider is not specified in CR
	idpName := rocketaihub.Spec.IdentityProvider.ExistingIdentityProvider
	if idpName == "" {
		idpName = rocketaihubIDPName
		ownerRef := []metav1.OwnerReference{
			{
				APIVersion:         "operator.ibm.com/v1alpha1",
				Kind:               "RocketAIHub",
				Name:               rocketaihub.Name,
				UID:                rocketaihub.UID,
				Controller:         &[]bool{true}[0],
				BlockOwnerDeletion: &[]bool{true}[0],
			},
		}
		// Install the Keycloak operator
		keycloakProject := projectv1.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:            keycloakProjectName,
				OwnerReferences: ownerRef,
			},
		}
		if err := r.Create(ctx, &keycloakProject); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		keycloakOperatorGroup := operatorsv1.OperatorGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:            keycloakProjectName,
				Namespace:       keycloakProjectName,
				OwnerReferences: ownerRef,
			},
			Spec: operatorsv1.OperatorGroupSpec{TargetNamespaces: []string{keycloakProjectName}},
		}
		if err := r.Create(ctx, &keycloakOperatorGroup); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		keycloakSubscription := operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:            keycloakSubscription,
				Namespace:       keycloakProjectName,
				OwnerReferences: ownerRef,
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Channel:                "stable",
				Package:                keycloakPackageName,
				CatalogSource:          "redhat-operators",
				CatalogSourceNamespace: "openshift-marketplace",
			},
		}
		if err := r.Create(ctx, &keycloakSubscription); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		// Wait for the Keycloak operator to be ready
		namespacedName := types.NamespacedName{
			Name:      keycloakPackageName,
			Namespace: keycloakProjectName,
		}
		if err := r.waitForObject(ctx, &appsv1.Deployment{}, namespacedName, retryInterval, timeout); err != nil {
			return err
		}
		// Create an instance of Keycloak
		keycloakCRPath := filepath.Join(keycloakPath, "keycloak-cr")
		if err := resources.CreateResources(ctx, r.Client, keycloakCRPath, nil); err != nil {
			return err
		}
		namespacedName = types.NamespacedName{
			Name:      "keycloak",
			Namespace: keycloakProjectName,
		}
		if err := r.waitForObject(ctx, &appsv1.StatefulSet{}, namespacedName, retryInterval, timeout); err != nil {
			return err
		}
		// Create a Keycloak realm for Rocket AI Hub
		var keycloakRealmPath string
		if rocketaihub.Spec.IdentityProvider.CreateDefaultUser {
			keycloakRealmPath = filepath.Join(keycloakPath, "keycloak-realm-with-user")
		} else {
			keycloakRealmPath = filepath.Join(keycloakPath, "keycloak-realm-without-user")
		}
		// Create a new environment variable for Keycloak client secret
		clientSecret, err := setClientSecret()
		if err != nil {
			return err
		}
		filePath := filepath.Join(keycloakRealmPath, "values.env")
		if err := resources.CreateResources(ctx, r.Client, keycloakRealmPath, []string{filePath}); err != nil {
			return err
		}
		// Create a secret which contains the keycloak client secret for the new identity provider
		idpClientSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "rocketaihub-client-secret",
				Namespace:       "openshift-config",
				OwnerReferences: ownerRef,
			},
			StringData: map[string]string{"clientSecret": clientSecret},
			Type:       corev1.SecretTypeOpaque,
		}
		if err := r.Create(ctx, &idpClientSecret); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		// Create a config map which contains the ca certificate of default ingress for the new identity provider
		routerCASecret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: "router-ca", Namespace: "openshift-ingress-operator"}, routerCASecret); err != nil {
			return err
		}
		caCert := string(routerCASecret.Data["tls.crt"])
		idpCAConfigMap := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            idpCAConfigMapName,
				Namespace:       "openshift-config",
				OwnerReferences: ownerRef,
			},
			Data: map[string]string{"ca.crt": caCert},
		}
		if err := r.Create(ctx, &idpCAConfigMap); err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
		// Add Keycloak as an identity provider of the OpenShift OAuth
		oAuth := &configv1.OAuth{}
		if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, oAuth); err != nil {
			return err
		}
		rocketaihubIDPExists := false
		for _, idp := range oAuth.Spec.IdentityProviders {
			if idp.Name == rocketaihubIDPName {
				rocketaihubIDPExists = true
				break
			}
		}
		if !rocketaihubIDPExists {
			clusterDomain, err := setClusterDomain(ctx, r.Client)
			if err != nil {
				return err
			}
			issuer := fmt.Sprintf("https://keycloak-%s.%s/auth/realms/%s", keycloakProjectName, clusterDomain, rocketaihubIDPName)
			idp := configv1.IdentityProvider{
				Name:          rocketaihubIDPName,
				MappingMethod: configv1.MappingMethodClaim,
				IdentityProviderConfig: configv1.IdentityProviderConfig{
					Type: configv1.IdentityProviderTypeOpenID,
					OpenID: &configv1.OpenIDIdentityProvider{
						ClientID:     rocketaihubIDPName,
						ClientSecret: configv1.SecretNameReference{Name: idpClientSecretName},
						CA:           configv1.ConfigMapNameReference{Name: idpCAConfigMapName},
						Issuer:       issuer,
						Claims: configv1.OpenIDClaims{
							PreferredUsername: []string{"email"},
							Name:              []string{"name"},
							Email:             []string{"email"},
						},
					},
				},
			}
			oAuth.Spec.IdentityProviders = append(oAuth.Spec.IdentityProviders, idp)
			if err := r.Update(ctx, oAuth); err != nil {
				return err
			}
		}
	}

	if err := r.configureUserConfig(ctx, idpName); err != nil {
		return err
	}

	return nil
}

// UserConfig is from Namespace Configuration Operator and used to create a Kubeflow Profile
// when a user logins via the identity provider specified in the UserConfig CR.
func (r *RocketAIHubReconciler) configureUserConfig(ctx context.Context, idpName string) error {
	// Patch the user config object using the new identity provider name
	userConfig := &redhatcop.UserConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: "kubeflow-user", Namespace: "kubeflow"}, userConfig); err != nil {
		return err
	}
	userConfig.Spec.ProviderName = idpName
	if err := r.Update(ctx, userConfig); err != nil {
		return err
	}

	return nil
}

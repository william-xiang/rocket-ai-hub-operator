package resources

import (
	"context"
	"os"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var logger = logr.Log.WithName("RocketAIHub Controller")

// Substitutes the values of environment variables in the manifests
func substituteEnv(ctx context.Context, client client.Client, yaml string) (string, error) {
	// Substitue the value of environment variable CLUSTER_DOMAIN for servicemesh
	if strings.Contains(yaml, "${CLUSTER_DOMAIN}") {
		ingress := &configv1.Ingress{}
		err := client.Get(ctx, types.NamespacedName{Name: "cluster"}, ingress)
		if err != nil {
			return "", err
		}
		os.Setenv("CLUSTER_DOMAIN", ingress.Spec.Domain)
		newYaml := os.ExpandEnv(yaml)
		return newYaml, nil
	}

	return yaml, nil
}

// Get all the resources from the given manifest path using kustomize
func getResources(manfiestPath string) ([]*resource.Resource, error) {
	// Get a kustomizer instance to deploy the applications using the manifests
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()

	// Create the ResMap for all the resources in the manifests
	resMap, err := kustomizer.Run(fs, manfiestPath)
	if err != nil {
		return nil, err
	}
	return resMap.Resources(), nil
}

// Get Unstructured object from Resource object
func getUnstructuredObj(ctx context.Context, client client.Client, resource *resource.Resource) (*unstructured.Unstructured, error) {
	// Convert Resource object to Unstructured object
	// Unstructured object has functioning TypeMeta features-- kind, version, etc.
	out := map[string]interface{}{}
	resourceYaml := resource.MustYaml()
	// Substitute the environment variables in the yaml file
	newResourceYaml, err := substituteEnv(ctx, client, resourceYaml)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal([]byte(newResourceYaml), out); err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: out}, nil
}

// Create Resources with provided manifests using kustomize
func CreateResources(ctx context.Context, client client.Client, manifestPath string) error {
	resources, err := getResources(manifestPath)
	if err != nil {
		return err
	}

	// Create all the resources
	for _, resource := range resources {
		if err := createResource(ctx, client, resource); err != nil {
			return err
		}
	}

	return nil
}

func createResource(ctx context.Context, client client.Client, ressource *resource.Resource) error {
	unstructured, err := getUnstructuredObj(ctx, client, ressource)
	if err != nil {
		return err
	}

	logger.Info("Creating resource", "Kind", unstructured.GetKind(), "Name", unstructured.GetName(), "Namespace", unstructured.GetNamespace())
	if err := client.Create(ctx, unstructured); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// Delete resources with provided manfiests using kustomize
func DeleteResources(ctx context.Context, client client.Client, manifestPath string) error {
	resources, err := getResources(manifestPath)
	if err != nil {
		return err
	}

	// Delete all the resources
	for _, resource := range resources {
		if err := deleteResource(ctx, client, resource); err != nil {
			return err
		}
	}

	return nil
}

func deleteResource(ctx context.Context, client client.Client, resource *resource.Resource) error {
	unstructured, err := getUnstructuredObj(ctx, client, resource)
	if err != nil {
		return err
	}

	logger.Info("Deleting resource", "Kind", unstructured.GetKind(), "Name", unstructured.GetName(), "Namespace", unstructured.GetNamespace())
	if err := client.Delete(ctx, unstructured); err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}

	return nil
}

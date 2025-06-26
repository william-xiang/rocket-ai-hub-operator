package resources

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var logger = logr.Log.WithName("RocketAIHub Controller")

// Get all the resources from the given manifest path using kustomize
func getResources(manifestPath string, filePaths []string) ([]*resource.Resource, error) {
	// Substitute the values of environment variables in files before creating the ResMap
	fileMap := make(map[string]string)
	for _, filePath := range filePaths {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		// Preserve the original content of the file
		oldContent := string(data)
		fileMap[filePath] = oldContent
		// Substitute the values of environment variables
		newContent := os.ExpandEnv(oldContent)
		err = os.WriteFile(filePath, []byte(newContent), 0644)
		if err != nil {
			return nil, err
		}
	}

	// Get a kustomizer instance to deploy the applications using the manifests
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()

	// Create the ResMap for all the resources in the manifests
	resMap, err := kustomizer.Run(fs, manifestPath)
	if err != nil {
		return nil, err
	}

	// Restore the file with preserved content
	for _, filePath := range filePaths {
		err = os.WriteFile(filePath, []byte(fileMap[filePath]), 0644)
		if err != nil {
			return nil, err
		}
	}

	return resMap.Resources(), nil
}

// Get Unstructured object from Resource object
func getUnstructuredObj(resource *resource.Resource) (*unstructured.Unstructured, error) {
	// Convert Resource object to Unstructured object
	// Unstructured object has functioning TypeMeta features-- kind, version, etc.
	out := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(resource.MustYaml()), out); err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: out}, nil
}

// Create Resources with provided manifests using kustomize
// Parameter filePaths contains path of files with references to environment variables of the form $VARIABLE or ${VARIABLE} being replaced with the corresponding values
func CreateResources(ctx context.Context, client client.Client, manifestPath string, filePaths []string) error {
	resources, err := getResources(manifestPath, filePaths)
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

func createResource(ctx context.Context, client client.Client, resource *resource.Resource) error {
	unstructured, err := getUnstructuredObj(resource)
	if err != nil {
		return err
	}

	logger.Info("Creating resource", "Kind", unstructured.GetKind(), "Name", unstructured.GetName(), "Namespace", unstructured.GetNamespace())
	if err := client.Create(ctx, unstructured); err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// Delete resources with provided manifests using kustomize
func DeleteResources(ctx context.Context, client client.Client, manifestPath string, filePaths []string) error {
	resources, err := getResources(manifestPath, filePaths)
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
	unstructured, err := getUnstructuredObj(resource)
	if err != nil {
		return err
	}

	logger.Info("Deleting resource", "Kind", unstructured.GetKind(), "Name", unstructured.GetName(), "Namespace", unstructured.GetNamespace())
	if err := client.Delete(ctx, unstructured); err != nil && !errors.IsNotFound(err) && !meta.IsNoMatchError(err) {
		return err
	}

	return nil
}

package components

import (
	"context"
	"os"
	"path/filepath"

	"github.com/IBM/rocketaihub-operator/pkg/resources"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ManifestRootPath = os.Getenv("MANIFEST_ROOT_PATH")
)

// Install all the components
func Install(ctx context.Context, client client.Client) error {
	manifestPath := filepath.Join(ManifestRootPath, "deployment")
	return resources.CreateResources(ctx, client, manifestPath)
}

// Uninstall the components
func Uninstall(ctx context.Context, client client.Client) error {
	manifestPath := filepath.Join(ManifestRootPath, "deployment")
	return resources.DeleteResources(ctx, client, manifestPath)
}

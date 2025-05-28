package kind

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"slices"

	"sigs.k8s.io/kind/pkg/cluster"
)

// Provider defines the interface for managing Kubernetes clusters using kind.
// It provides methods to create, delete, check existence of clusters, and retrieve kubeconfig.
type Provider interface {
	// CreateCluster creates a new Kubernetes cluster with the given name.
	CreateCluster(name string) error

	// DeleteCluster deletes the Kubernetes cluster with the given name.
	DeleteCluster(name string) error

	// ClusterExists checks if a Kubernetes cluster with the given name exists.
	ClusterExists(name string) (bool, error)

	// KubeConfig retrieves the kubeconfig for the specified cluster name.
	KubeConfig(name string) (string, error)
}

var (
	kubeconfigPath = path.Join(os.TempDir(), "cluster-provider-kind.kubeconfig")
)

// KindProvider returns a new instance of the kind provider for managing Kubernetes clusters.
// It uses the default Docker-based kind provider configuration.
func NewKindProvider() Provider {
	return &kindProvider{
		internal: cluster.NewProvider(
			cluster.ProviderWithDocker(),
		),
	}
}

var _ Provider = &kindProvider{}

type kindProvider struct {
	internal *cluster.Provider
}

// ClusterExists implements Provider.
func (p *kindProvider) ClusterExists(name string) (bool, error) {
	clusters, err := p.internal.List()
	if err != nil {
		return false, err
	}

	return slices.Contains(clusters, name), nil
}

// CreateCluster implements Provider.
func (p *kindProvider) CreateCluster(name string) error {
	options := []cluster.CreateOption{
		cluster.CreateWithWaitForReady(1 * time.Minute),
		cluster.CreateWithKubeconfigPath(kubeconfigPath),
	}
	return p.internal.Create(name, options...)
}

// DeleteCluster implements Provider.
func (p *kindProvider) DeleteCluster(name string) error {
	return p.internal.Delete(name, kubeconfigPath)
}

// KubeConfig implements Provider.
func (p *kindProvider) KubeConfig(name string) (string, error) {
	kubeconfigStr, err := p.internal.KubeConfig(name, false) // TODO: false only in local mode
	if err != nil {
		return "", err
	}

	containerName := p.controlPlaneContainer(name)

	containerIP, err := getDockerContainerIP(containerName)
	if err != nil {
		return "", err
	}

	return strings.ReplaceAll(kubeconfigStr, "https://"+containerName, "https://"+containerIP.String()), nil
}

func (p *kindProvider) controlPlaneContainer(name string) string {
	return fmt.Sprintf("%s-control-plane", name)
}

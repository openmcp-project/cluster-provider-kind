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

type Provider interface {
	CreateCluster(name string) error
	DeleteCluster(name string) error
	ClusterExists(name string) (bool, error)
	KubeConfig(name string) (string, error)
}

var (
	kubeconfigPath = path.Join(os.TempDir(), "cluster-provider-kind.kubeconfig")
)

func NewDockerProvider() Provider {
	return &provider{
		internal: cluster.NewProvider(
			cluster.ProviderWithDocker(),
		),
	}
}

var _ Provider = &provider{}

type provider struct {
	internal *cluster.Provider
}

// ClusterExists implements Provider.
func (p *provider) ClusterExists(name string) (bool, error) {
	clusters, err := p.internal.List()
	if err != nil {
		return false, err
	}

	return slices.Contains(clusters, name), nil
}

// CreateCluster implements Provider.
func (p *provider) CreateCluster(name string) error {
	options := []cluster.CreateOption{
		cluster.CreateWithWaitForReady(1 * time.Minute),
		cluster.CreateWithKubeconfigPath(kubeconfigPath),
	}
	return p.internal.Create(name, options...)
}

// DeleteCluster implements Provider.
func (p *provider) DeleteCluster(name string) error {
	return p.internal.Delete(name, kubeconfigPath)
}

func (p *provider) KubeConfig(name string) (string, error) {
	kubeconfigStr, err := p.internal.KubeConfig(name, true)
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

func (p *provider) controlPlaneContainer(name string) string {
	return fmt.Sprintf("%s-control-plane", name)
}

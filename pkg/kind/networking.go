package kind

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	networkName = "kind"
	subnetMin   = 200
	subnetMax   = 255
)

var (
	errIPv4NetworkNotFound = errors.New("ipv4 network not found")
	errUnsupportedNetwork  = errors.New("unsupported network")
	errNoSubnetsAvailable  = errors.New("no subnets available")
	errInvalidIP           = errors.New("invalid textual representation of an IP address")

	AnnotationAssignedSubnet = v1alpha1.GroupVersion.Group + "/assigned-subnet"
	lockListClusters         = sync.Mutex{}
)

func getDockerContainerIP(containerName string) (net.IP, error) {
	cmd := exec.Command("docker", "container", "inspect", "-f", "{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerName)
	cmdOut, err := cmd.Output()
	if err != nil {
		return net.IP{}, err
	}

	parsed := net.ParseIP(strings.TrimSpace(string(cmdOut)))
	if parsed == nil {
		return net.IP{}, errInvalidIP
	}

	return parsed, nil
}

func GetDockerV4Network(ctx context.Context) (net.IPNet, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", "-f", "json", networkName)
	cmdOut, err := cmd.Output()
	if err != nil {
		return net.IPNet{}, err
	}

	return parseDockerV4Network(cmdOut)
}

func parseDockerV4Network(cmdOut []byte) (net.IPNet, error) {
	networks := []Network{}
	if err := json.Unmarshal(cmdOut, &networks); err != nil {
		return net.IPNet{}, err
	}

	for _, cfg := range networks[0].IPAM.Config {
		_, parsedNet, err := net.ParseCIDR(cfg.Subnet)
		if err != nil {
			return net.IPNet{}, err
		}

		if isIPv4(parsedNet) {
			return *parsedNet, nil
		}
	}
	return net.IPNet{}, errIPv4NetworkNotFound
}

// isIPv4 checks if the network is IPv4
func isIPv4(ipNet *net.IPNet) bool {
	return ipNet.IP.To4() != nil
}

func NextAvailableLBNetwork(ctx context.Context, c client.Client) (net.IPNet, error) {
	lockListClusters.Lock()
	defer lockListClusters.Unlock()

	kindNetwork, err := GetDockerV4Network(ctx)
	if err != nil {
		return net.IPNet{}, err
	}

	clusters := &v1alpha1.ClusterList{}
	if err := c.List(ctx, clusters); err != nil {
		return net.IPNet{}, err
	}

	for i := subnetMin; i <= subnetMax; i++ {
		subnet, err := calculateV4Subnet(kindNetwork, i)
		if err != nil {
			return net.IPNet{}, err
		}

		taken, err := isIpNetTaken(subnet, clusters)
		if err != nil {
			return net.IPNet{}, err
		}
		if taken {
			continue
		}
		return subnet, nil
	}
	return net.IPNet{}, errNoSubnetsAvailable
}

// calculateV4Subnet returns a subnet of the given net.IPNet. Must be a /8 or /16 network.
func calculateV4Subnet(input net.IPNet, offset int) (net.IPNet, error) {
	// Make sure we are dealing with a 4-byte representation.
	inputV4 := input.IP.To4()
	ones, bits := input.Mask.Size()

	// Subnet mask should be either 8 or 16 out of 32
	if !(ones == 8 || ones == 16) || bits != 32 {
		return net.IPNet{}, errUnsupportedNetwork
	}

	subnetIP := slices.Clone(inputV4)
	subnetIP[2] = byte(offset)

	return net.IPNet{
		IP:   subnetIP,
		Mask: net.CIDRMask(24, 32),
	}, nil
}

func isIpNetTaken(ipnet net.IPNet, clusters *v1alpha1.ClusterList) (bool, error) {
	for _, c := range clusters.Items {
		cNet, err := SubnetFromCluster(&c)
		if err != nil {
			return false, err
		}
		if cNet == nil {
			continue
		}
		if cNet.IP.Equal(ipnet.IP) {
			return true, nil
		}
	}
	return false, nil
}

func SubnetFromCluster(c *v1alpha1.Cluster) (*net.IPNet, error) {
	ipNetStr, ok := c.Annotations[AnnotationAssignedSubnet]
	if !ok {
		return nil, nil
	}

	_, parsedNet, err := net.ParseCIDR(ipNetStr)
	return parsedNet, err
}

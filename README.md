[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/cluster-provider-kind)](https://api.reuse.software/info/github.com/openmcp-project/cluster-provider-kind)

# Cluster Provider kind

## About this project

A cluster provider for [OpenMCP](https://github.com/openmcp-project/openmcp-operator) that uses [kind](https://kind.sigs.k8s.io/) (Kubernetes IN Docker) to provision and manage Kubernetes clusters. This provider enables you to create and manage multiple Kubernetes clusters running as Docker containers, making it ideal for:

- **Local Development**: Quickly spin up multiple clusters for testing multi-cluster scenarios
- **E2E Testing**: Automated testing of multi-cluster applications and operators
- **CI/CD Pipelines**: Lightweight cluster provisioning for testing environments

## Prerequisites

Before using this cluster provider, ensure you have:

- **Docker**: Running Docker daemon with socket accessible
- **kind**: [kind CLI tool](https://kind.sigs.k8s.io/docs/user/quick-start/) installed
- **kubectl**: For interacting with Kubernetes clusters

## Installation

### Production Deployment

In combination with the [OpenMCP Operator](https://github.com/openmcp-project/openmcp-operator), this operator can be deployed via a simple Kubernetes resource:

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: ClusterProvider
metadata:
  name: kind
spec:
  image: "ghcr.io/openmcp-project/images/cluster-provider-kind:<latest-version>"
```

### Local Development

To run the operator locally for development:

1. **Start a platform kind cluster**:
```shell
kind create cluster --name platform
kubectl config use-context kind-platform
```
2. Install the Platform CRDs of the openmcp-operator:
Apply the CRDs from the OpenMCP operator repository [here](https://github.com/openmcp-project/openmcp-operator/tree/main/api/crds/manifests).

3. **Initialize the CRDs**:
```shell
go run ./cmd/cluster-provider-kind/main.go init
```

1. **Run the operator**:
```shell
go run ./cmd/cluster-provider-kind/main.go run
```

## Usage Examples

### Creating a Cluster

Create a new kind cluster by applying a Cluster resource:

```yaml
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: Cluster
metadata:
  name: my-managedcontrolplane
  namespace: default
spec:
  profile: kind  # This tells the kind provider to handle this cluster
  tenancy: Exclusive
```

```shell
kubectl apply -f cluster.yaml
```

### Requesting Access to a Cluster

Create an AccessRequest to get kubeconfig for a cluster:

```yaml
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: AccessRequest
metadata:
  name: my-access
  namespace: default
spec:
  clusterRef:
    name: my-managedcontrolplane
    namespace: default
  permissions: []
```

The kubeconfig will be stored in a Secret in the same namespace as the `AccessRequest`.

## How it works

### Docker Socket Access

In order to create new kind clusters from within a kind cluster, the Docker socket (usually `/var/run/docker.sock`) needs to be available to the `cluster-provider-kind` pod. As a prerequisite, the Docker socket of the host machine must be mounted into the nodes of the platform kind cluster. In this case, there is only a single node (`platform-control-plane`). The socket can then be mounted by the cluster-provider-kind pod using a `hostPath` volume.

```mermaid
flowchart TD

subgraph HostMachine
    DockerSocket
    subgraph "platform-control-plane"
        /var/run/docker.sock
        cluster-provider-kind
    end

    DockerSocket -- extraMount --> /var/run/docker.sock
    /var/run/docker.sock -- volumeMount --> cluster-provider-kind

    subgraph mcp-control-plane
        SomeResource
    end

    subgraph mcp-workload
        SomePod
    end

    cluster-provider-kind -- creates --> mcp-control-plane
    cluster-provider-kind -- creates --> mcp-workload
end
style HostMachine fill:#eee
```

### Platform Cluster Configuration

The kind configuration for the platform cluster may look like this:

```yaml
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/docker.sock
```

### Testing Docker Socket Access

In order to test that the socket is functional, a simple pod can be deployed:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ubuntu
spec:
  containers:
  - image: ubuntu
    name: ubuntu
    volumeMounts:
      - mountPath: /var/run/docker.sock
        name: docker
    command:
      - sleep
      - "3600"
  volumes:
    - name: docker
      hostPath:
        path: /var/run/docker.sock
        type: Socket
```

After installing docker CLI and kind, it should be possible to create a new kind cluster on the level of the host machine: `kind create cluster --name test`

```
$ kind create cluster --name test

Creating cluster "test" ...
 ✓ Ensuring node image (kindest/node:v1.31.0) 🖼
 ✓ Preparing nodes 📦
 ✓ Writing configuration 📜
 ✓ Starting control-plane 🕹️
 ✓ Installing CNI 🔌
 ✓ Installing StorageClass 💾
Set kubectl context to "kind-test"
You can now use your cluster with:

kubectl cluster-info --context kind-test

Thanks for using kind! 😊
```

This can be verified by running `kind get clusters` directly on the host machine:

```
$ kind get clusters

platform
test
```

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/cluster-provider-kind/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/cluster-provider-kind/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and cluster-provider-kind contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/cluster-provider-kind).

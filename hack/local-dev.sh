#!/bin/bash
set -e -o pipefail

# Image and version defaults - override by exporting variables in your shell

# ============================================================================
# OpenMCP Operator
# ============================================================================
OPENMCP_OPERATOR_VERSION=${OPENMCP_OPERATOR_VERSION:-v0.17.1}
OPENMCP_OPERATOR_IMAGE=${OPENMCP_OPERATOR_IMAGE:-ghcr.io/openmcp-project/images/openmcp-operator:${OPENMCP_OPERATOR_VERSION}}
OPENMCP_ENVIRONMENT=${OPENMCP_ENVIRONMENT:-debug}

# ============================================================================
# Cluster Provider Kind
# ============================================================================
OPENMCP_CP_KIND_VERSION=${OPENMCP_CP_KIND_VERSION:-$(task version)-linux-$(go env GOARCH)}
OPENMCP_CP_KIND_IMAGE=${OPENMCP_CP_KIND_IMAGE:-ghcr.io/openmcp-project/images/cluster-provider-kind:v0.0.15}

# ============================================================================
# Service Providers
# ============================================================================
SERVICE_PROVIDER_CROSSPLANE_IMAGE=${SERVICE_PROVIDER_CROSSPLANE_IMAGE:-ghcr.io/openmcp-project/images/service-provider-crossplane:v0.1.5}
SERVICE_PROVIDER_LANDSCAPER_IMAGE=${SERVICE_PROVIDER_LANDSCAPER_IMAGE:-ghcr.io/openmcp-project/images/service-provider-landscaper:v0.12.0}

# ============================================================================
# Platform Service Gateway
# ============================================================================
PLATFORM_SERVICE_GATEWAY_IMAGE=${PLATFORM_SERVICE_GATEWAY_IMAGE:-ghcr.io/openmcp-project/images/platform-service-gateway:v0.0.4}

ENVOY_PROXY_IMAGE=${ENVOY_PROXY_IMAGE:-ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-proxy:distroless-v1.36.2}
ENVOY_GATEWAY_IMAGE=${ENVOY_GATEWAY_IMAGE:-ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-gateway:v1.5.4}
ENVOY_RATELIMIT_IMAGE=${ENVOY_RATELIMIT_IMAGE:-ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/images/envoy-ratelimit:99d85510}
ENVOY_GATEWAY_CHART_TAG=${ENVOY_GATEWAY_CHART_TAG:-1.5.4}
ENVOY_GATEWAY_CHART_URL=${ENVOY_GATEWAY_CHART_URL:-oci://ghcr.io/openmcp-project/components/github.com/openmcp-project/openmcp/charts/envoy-gateway}

# ============================================================================
# Crossplane Provider Configuration
# ============================================================================
CROSSPLANE_VERSION=${CROSSPLANE_VERSION:-v1.20.0}
CROSSPLANE_CHART_URL=${CROSSPLANE_CHART_URL:-ghcr.io/openmcp-project/openmcp/charts/crossplane:1.20.0}
CROSSPLANE_IMAGE=${CROSSPLANE_IMAGE:-xpkg.crossplane.io/crossplane/crossplane:1.20.0}
CROSSPLANE_PROVIDER_KUBERNETES_VERSIONS=${CROSSPLANE_PROVIDER_KUBERNETES_VERSIONS:-v0.16.0,v0.15.0}

# ============================================================================
# Landscaper Provider Configuration
# ============================================================================
LANDSCAPER_REPOSITORY=${LANDSCAPER_REPOSITORY:-europe-docker.pkg.dev/sap-gcp-cp-k8s-stable-hub/landscaper}
LANDSCAPER_VERSIONS=${LANDSCAPER_VERSIONS:-v0.136.0,v0.137.0}

# ============================================================================
# Flux2
# ============================================================================
FLUX2_INSTALL_URL=${FLUX2_INSTALL_URL:-https://github.com/fluxcd/flux2/releases/latest/download/install.yaml}

# ============================================================================
# Service Provider Deployment Control
# ============================================================================
DEPLOY_SP_CROSSPLANE=${DEPLOY_SP_CROSSPLANE:-true}
DEPLOY_SP_LANDSCAPER=${DEPLOY_SP_LANDSCAPER:-true}
DEPLOY_SP_GATEWAY=${DEPLOY_SP_GATEWAY:-true}

# ============================================================================
# Setup Functions
# ============================================================================

log_info() {
  echo "[INFO] $*"
}

log_section() {
  echo ""
  echo "=========================================================================="
  echo "[STEP] $*"
  echo "=========================================================================="
}

setup_kind_cluster() {
  log_section "Creating platform KinD cluster"
  kind create cluster --name platform --config - << EOF
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/host-docker.sock
EOF
}

pull_docker_images() {
  log_section "Pulling required Docker images"
  docker image inspect ${OPENMCP_OPERATOR_IMAGE} > /dev/null || docker image pull ${OPENMCP_OPERATOR_IMAGE}
  docker image inspect ${OPENMCP_CP_KIND_IMAGE} > /dev/null || docker image pull ${OPENMCP_CP_KIND_IMAGE}
}

load_images_to_kind() {
  log_section "Loading images into KinD cluster"
  kind load docker-image --name platform ${OPENMCP_OPERATOR_IMAGE}
  kind load docker-image --name platform ${OPENMCP_CP_KIND_IMAGE}
}

setup_openmcp_namespace_and_rbac() {
  log_section "Setting up OpenMCP namespace and required RBAC"
  kubectl apply -f - << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: openmcp-system
spec: {}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: openmcp-operator
  namespace: openmcp-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: openmcp-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: openmcp-operator
  namespace: openmcp-system
EOF
}

setup_openmcp_configmap() {
  log_section "Creating OpenMCP operator ConfigMap"
  kubectl apply -f - << EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: openmcp-operator
  namespace: openmcp-system
data:
  config: |
    managedControlPlane:
      mcpClusterPurpose: mcp
    scheduler:
      scope: Cluster
      purposeMappings:
        mcp:
          template:
            spec:
              profile: kind
              tenancy: Exclusive
        platform:
          template:
            spec:
              profile: kind
              tenancy: Shared
        onboarding:
          template:
            spec:
              profile: kind
              tenancy: Shared
        workload:
          template:
            spec:
              profile: kind
              tenancy: Shared
EOF
}

setup_openmcp_operator_deployment() {
  log_section "Deploying OpenMCP operator"
  kubectl apply -f - << EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: openmcp-operator
  namespace: openmcp-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: openmcp-operator
  template:
    metadata:
      labels:
        app: openmcp-operator
    spec:
      serviceAccountName: openmcp-operator
      initContainers:
      - image: ${OPENMCP_OPERATOR_IMAGE}
        name: openmcp-operator-init
        resources: {}
        args:
        - init
        - --environment
        - ${OPENMCP_ENVIRONMENT}
        - --config
        - /etc/openmcp-operator/config
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
        - name: POD_IP
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: status.podIP
        - name: POD_SERVICE_ACCOUNT_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.serviceAccountName
        volumeMounts:
        - name: config
          mountPath: /etc/openmcp-operator
          readOnly: true
      containers:
      - image: ${OPENMCP_OPERATOR_IMAGE}
        name: openmcp-operator
        resources: {}
        args:
        - run
        - --environment
        - ${OPENMCP_ENVIRONMENT}
        - --config
        - /etc/openmcp-operator/config
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: metadata.namespace
        - name: POD_IP
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: status.podIP
        - name: POD_SERVICE_ACCOUNT_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.serviceAccountName
        volumeMounts:
        - name: config
          mountPath: /etc/openmcp-operator
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: openmcp-operator
EOF
}

setup_cluster_provider() {
  log_section "Installing ClusterProvider for KinD"
  kubectl wait --for=create customresourcedefinitions.apiextensions.k8s.io/clusterproviders.openmcp.cloud --timeout=60s
  kubectl apply -f - << EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ClusterProvider
metadata:
  name: kind
spec:
  image: ${OPENMCP_CP_KIND_IMAGE}
  extraVolumes:
  - name: docker-socket
    hostPath:
      path: /var/run/host-docker.sock
      type: Socket
  extraVolumeMounts:
  - name: docker-socket
    mountPath: /var/run/docker.sock
EOF
}

setup_platform_cluster_resource() {
  log_section "Creating platform Cluster resource"
  kubectl apply -f - << EOF
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: Cluster
metadata:
  annotations:
    kind.clusters.openmcp.cloud/name: platform
  name: platform
  namespace: openmcp-system
spec:
  kubernetes: {}
  profile: kind
  purposes:
  - platform
  tenancy: Shared
EOF
}

setup_service_providers() {
  log_section "Installing service providers"
  
  # Deploy Crossplane if enabled
  if [[ "$DEPLOY_SP_CROSSPLANE" == "true" ]]; then
    log_info "Installing Crossplane service provider"
    kubectl apply -f - << EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: crossplane
spec:
  image: ${SERVICE_PROVIDER_CROSSPLANE_IMAGE}
EOF
    log_info "Waiting for Crossplane service provider initialization..."
    kubectl wait --for=create -n openmcp-system job/sp-crossplane-init --timeout=120s
    kubectl wait --for=condition=complete -n openmcp-system job/sp-crossplane-init --timeout=120s
  fi
  
  # Deploy Landscaper if enabled
  if [[ "$DEPLOY_SP_LANDSCAPER" == "true" ]]; then
    log_info "Installing Landscaper service provider"
    kubectl apply -f - << EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: landscaper
spec:
  image: ${SERVICE_PROVIDER_LANDSCAPER_IMAGE}
EOF
    log_info "Waiting for Landscaper service provider initialization..."
    kubectl wait --for=create -n openmcp-system job/sp-landscaper-init --timeout=120s
    kubectl wait --for=condition=complete -n openmcp-system job/sp-landscaper-init --timeout=120s
  fi
  
  # Deploy Gateway if enabled
  if [[ "$DEPLOY_SP_GATEWAY" == "true" ]]; then
    log_info "Installing Gateway platform service"
    kubectl apply -f - << EOF
apiVersion: openmcp.cloud/v1alpha1
kind: PlatformService
metadata:
  name: gateway
spec:
  image: ${PLATFORM_SERVICE_GATEWAY_IMAGE}
EOF
    log_info "Waiting for Gateway platform service initialization..."
    kubectl wait --for=create -n openmcp-system job/ps-gateway-init --timeout=120s
    kubectl wait --for=condition=complete -n openmcp-system job/ps-gateway-init --timeout=120s
  fi
}

setup_provider_configs() {
  log_section "Configuring service providers"
  
  # Deploy Landscaper provider config if enabled
  if [[ "$DEPLOY_SP_LANDSCAPER" == "true" ]]; then
    log_info "Installing Landscaper provider configuration"
    kubectl apply -f - << EOF
apiVersion: landscaper.services.openmcp.cloud/v1alpha2
kind: ProviderConfig
metadata:
  name: default
  labels:
    landscaper.services.openmcp.cloud/providertype: default
spec:
  deployment:
    repository: ${LANDSCAPER_REPOSITORY}
    availableVersions:
      - v0.142.0
      
EOF
  fi
  
  # Deploy Gateway provider config if enabled
  if [[ "$DEPLOY_SP_GATEWAY" == "true" ]]; then
    log_info "Installing Gateway service provider configuration"
    kubectl apply -f - << EOF
apiVersion: gateway.openmcp.cloud/v1alpha1
kind: GatewayServiceConfig
metadata:
  name: gateway
spec:
  envoyGateway:
    images:
      proxy: "${ENVOY_PROXY_IMAGE}"
      gateway: "${ENVOY_GATEWAY_IMAGE}"
      rateLimit: "${ENVOY_RATELIMIT_IMAGE}"
    chart:
      url: "${ENVOY_GATEWAY_CHART_URL}"
      tag: "${ENVOY_GATEWAY_CHART_TAG}"

  clusters:
    - selector:
        matchPurpose: platform
    - selector:
        matchPurpose: workload

  dns:
    baseDomain: openmcp.cluster.local
EOF
  fi

  # Deploy Crossplane provider config if enabled
  if [[ "$DEPLOY_SP_CROSSPLANE" == "true" ]]; then
    log_info "Installing Crossplane provider configuration"
    kubectl apply -f - << EOF
apiVersion: crossplane.services.openmcp.cloud/v1alpha1
kind: ProviderConfig
metadata:
  name: default
spec:
  versions:
    - version: ${CROSSPLANE_VERSION}
      chart:
        url: "${CROSSPLANE_CHART_URL}"
       
      image:
        url: "${CROSSPLANE_IMAGE}"
        
  providers:
    availableProviders:
      - name: provider-kubernetes
        package: xpkg.upbound.io/upbound/provider-kubernetes
        versions:
          - v0.16.0
          - v0.15.0
EOF
  fi
}

setup_flux() {
  log_section "Installing Flux2"
  kubectl apply -f ${FLUX2_INSTALL_URL}
}

wait_for_onboarding_cluster() {
  log_section "Waiting for onboarding cluster to be ready"
  kubectl wait --for=create -n openmcp-system cluster/onboarding --timeout=60s
  kubectl wait --for='jsonpath={.status.conditions[?(@.type=="Ready")].status}=True' -n openmcp-system cluster/onboarding --timeout=120s
  kind export kubeconfig --name "$(kind get clusters | grep onboarding -m 1)"
}

setup_managed_control_plane() {
  log_section "Setting up managed control plane"
  kubectl apply -f - << EOF
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: test
spec:
  iam: {}
EOF
}


check_inotify_limits() {
  log_section "Checking system inotify limits"
  
  local default_max_user_watches=8192
  local max_user_watches
  max_user_watches=$(cat /proc/sys/fs/inotify/max_user_watches 2>/dev/null) || max_user_watches="0"
  
  local max_user_instances
  max_user_instances=$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null) || max_user_instances="0"
  
  if [[ $max_user_watches -le $default_max_user_watches ]]; then
    echo ""
    echo "[WARNING] Low inotify limits detected"
    echo "Current: watches=$max_user_watches, instances=$max_user_instances"
    echo "Recommended: watches>65536, instances>256"
    echo "Fix: sudo sysctl -w fs.inotify.max_user_watches=524288"
    echo ""
  fi
}

# ============================================================================
# Utility Functions
# ============================================================================

show_help() {
  cat << EOF
Usage: $(basename "$0") [COMMAND] [OPTIONS]

Commands:
  deploy              Deploy OpenMCP local development environment
  reset [OPTIONS]     Delete all KinD clusters
  help                Show this help message

Options for reset:
  --force             Skip confirmation prompt when deleting clusters

Examples:
  $(basename "$0") deploy              # Deploy everything
  $(basename "$0") reset               # Delete clusters (with confirmation)
  $(basename "$0") reset --force       # Delete clusters (skip confirmation)
  $(basename "$0") help                # Show this help message

Environment Variables:

Service Provider Deployment Control (default: all true):
  DEPLOY_SP_CROSSPLANE    # Enable/disable Crossplane service provider
  DEPLOY_SP_LANDSCAPER    # Enable/disable Landscaper service provider
  DEPLOY_SP_GATEWAY       # Enable/disable Gateway platform service

You can override default image versions by exporting environment variables:
  OPENMCP_OPERATOR_VERSION
  OPENMCP_OPERATOR_IMAGE
  OPENMCP_CP_KIND_IMAGE
  SERVICE_PROVIDER_CROSSPLANE_IMAGE
  SERVICE_PROVIDER_LANDSCAPER_IMAGE
  PLATFORM_SERVICE_GATEWAY_IMAGE
  ENVOY_PROXY_IMAGE
  ENVOY_GATEWAY_IMAGE
  ENVOY_RATELIMIT_IMAGE
  CROSSPLANE_VERSION
  FLUX2_INSTALL_URL

EOF
}

reset_clusters() {
  local force=false
  
  # Check for --force flag
  if [[ "$1" == "--force" ]]; then
    force=true
  fi
  
  log_section "Resetting OpenMCP local development environment"
  
  # Get list of clusters
  local clusters
  clusters=$(kind get clusters 2>/dev/null || true)
  
  if [[ -z "$clusters" ]]; then
    log_info "No KinD clusters found"
    return 0
  fi
  
  log_info "Found clusters: $clusters"
  
  if [[ "$force" == false ]]; then
    echo ""
    echo "WARNING: This will delete all KinD clusters:"
    echo "$clusters"
    echo ""
    read -p "Are you sure you want to delete all clusters? (yes/no): " confirmation
    
    if [[ "$confirmation" != "yes" ]]; then
      log_info "Cluster deletion cancelled"
      return 0
    fi
  fi
  
  log_info "Deleting all KinD clusters..."
  kind delete clusters --all
  log_info "All clusters deleted successfully"
}

deploy_environment() {
  log_section "Starting OinK setup"

  setup_kind_cluster
  pull_docker_images
  load_images_to_kind
  setup_openmcp_namespace_and_rbac
  setup_openmcp_configmap
  setup_openmcp_operator_deployment
  setup_cluster_provider
  setup_platform_cluster_resource
  setup_service_providers
  setup_provider_configs
  setup_flux
  wait_for_onboarding_cluster
  setup_managed_control_plane
  check_inotify_limits

  log_section "OpenMCP local development setup completed successfully"
}

# ============================================================================
# Main Execution
# ============================================================================

# Require explicit command
COMMAND="${1}"

if [[ -z "$COMMAND" ]]; then
  show_help
  exit 0
fi

case "$COMMAND" in
  deploy)
    deploy_environment
    ;;
  reset)
    reset_clusters "$2"
    ;;
  help|--help|-h)
    show_help
    ;;
  *)
    echo "Error: Unknown command '$COMMAND'" >&2
    echo "" >&2
    show_help
    exit 1
    ;;
esac


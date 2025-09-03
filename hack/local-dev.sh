kind delete clusters --all

OPENMCP_OPERATOR_VERSION=v0.13.0
OPENMCP_OPERATOR_IMAGE=ghcr.io/openmcp-project/images/openmcp-operator:${OPENMCP_OPERATOR_VERSION}

OPENMCP_CP_KIND_VERSION=v0.0.12-dev-833b7e03cf1205f4405d21eaf1524d9e5bd29373-linux-amd64
OPENMCP_CP_KIND_IMAGE=ghcr.io/openmcp-project/images/cluster-provider-kind:${OPENMCP_CP_KIND_VERSION}

OPENMCP_ENVIRONMENT=debug

# Create platform cluster
kind create cluster --name platform --config - << EOF
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
nodes:
- role: control-plane
  extraMounts:
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/host-docker.sock
EOF

# Load images
kind load docker-image --name platform ${OPENMCP_OPERATOR_IMAGE}
kind load docker-image --name platform ${OPENMCP_CP_KIND_IMAGE}

# Create openmcp-system Namespace
kubectl apply -f - << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: openmcp-system
spec: {}
EOF

# Create openmcp-operator ServiceAccount
kubectl apply -f - << EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: openmcp-operator
  namespace: openmcp-system
EOF

# Create ClusterRoleBinding for openmcp-operator
kubectl apply -f - << EOF
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

# Create ConfigMap for openmcp-operator
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
EOF

# Create openmcp-operator Deployment
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

# Wait for ClusterProvider CRD to be created
kubectl wait --for=create customresourcedefinitions.apiextensions.k8s.io/clusterproviders.openmcp.cloud --timeout=30s

# Install ClusterProvider for kind
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

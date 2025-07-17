package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinery "k8s.io/apimachinery/pkg/types"
)

type RequestPhase string

const (
	// AccessRequestPending is the phase if the AccessRequest has not been scheduled yet.
	AccessRequestPending RequestPhase = "Pending"
	// AccessRequestGranted is the phase if the AccessRequest has been granted.
	AccessRequestGranted RequestPhase = "Granted"
)

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.clusterRef) || has(self.clusterRef)", message="clusterRef may not be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.requestRef) || has(self.requestRef)", message="requestRef may not be removed once set"
type AccessRequestSpec struct {
	// ClusterRef is the reference to the Cluster for which access is requested.
	// If set, requestRef will be ignored.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterRef is immutable"
	// +optional
	ClusterRef *ObjectReference `json:"clusterRef,omitempty"`

	// RequestRef is the reference to the ClusterRequest for whose Cluster access is requested.
	// Is ignored if clusterRef is set.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="requestRef is immutable"
	// +optional
	RequestRef *ObjectReference `json:"requestRef,omitempty"`

	// Permissions are the requested permissions.
	Permissions []PermissionsRequest `json:"permissions"`
}

type PermissionsRequest struct {
	// Namespace is the namespace for which the permissions are requested.
	// If empty, this will result in a ClusterRole, otherwise in a Role in the respective namespace.
	// Note that for a Role, the namespace needs to either exist or a permission to create it must be included in the requested permissions (it will be created automatically then), otherwise the request will be rejected.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Rules are the requested RBAC rules.
	Rules []rbacv1.PolicyRule `json:"rules"`
}

// AccessRequestStatus defines the observed state of AccessRequest
type AccessRequestStatus struct {
	// Conditions contains the conditions.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is the current phase of the request.
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Enum=Pending;Granted;Denied
	Phase RequestPhase `json:"phase"`

	// SecretRef holds the reference to the secret that contains the actual credentials.
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ar;areq
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// AccessRequest is the Schema for the accessrequests API
type AccessRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccessRequestSpec   `json:"spec,omitempty"`
	Status AccessRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessRequestList contains a list of AccessRequest
type AccessRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccessRequest{}, &AccessRequestList{})
}

// ObjectReference is a reference to an object in any namespace.
type ObjectReference apimachinery.NamespacedName

// LocalObjectReference is a reference to an object in the same namespace as the resource referencing it.
type LocalObjectReference corev1.LocalObjectReference

// SecretReference is a reference to a secret in any namespace with a key.
type SecretReference struct {
	ObjectReference `json:",inline"`
	// Key is the key in the secret to use.
	Key string `json:"key"`
}

// LocalSecretReference is a reference to a secret in the same namespace as the resource referencing it with a key.
type LocalSecretReference struct {
	LocalObjectReference `json:",inline"`
	// Key is the key in the secret to use.
	Key string `json:"key"`
}

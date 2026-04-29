package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterStatus is the provider-specific status for a kind cluster.
type ClusterStatus struct {
	metav1.TypeMeta `json:",inline"`

	// KindClusterName is the name of the underlying kind cluster.
	KindClusterName string `json:"kindClusterName"`
}

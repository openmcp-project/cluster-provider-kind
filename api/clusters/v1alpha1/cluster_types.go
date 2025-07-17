/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// ClusterSpec defines the desired state of Cluster.
type ClusterSpec struct{}

// ClusterStatus defines the observed state of Cluster.
type ClusterStatus struct {
	// Reason is expected to contain a CamelCased string that provides further information in a machine-readable format.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message contains further details in a human-readable format.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions is a list of conditions that apply to the cluster.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`

	// +kubebuilder:default=Unknown
	Phase string `json:"phase"`

	// APIServer is the API server endpoint of the cluster.
	// +optional
	APIServer string `json:"apiServer,omitempty"`

	// ProviderStatus is the provider-specific status of the cluster.
	// x-kubernetes-preserve-unknown-fields: true
	// +optional
	ProviderStatus *runtime.RawExtension `json:"providerStatus,omitempty"`
}

// Cluster is the Schema for the clusters API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}

const (
	// StatusPhaseReady indicates that the resource is ready. All conditions are met and are in status "True".
	StatusPhaseReady = "Ready"
	// StatusPhaseProgressing indicates that the resource is not ready and being created or updated. At least one condition is not met and is in status "False".
	StatusPhaseProgressing = "Progressing"
	// StatusPhaseTerminating indicates that the resource is not ready and in deletion. At least one condition is not met and is in status "False".
	StatusPhaseTerminating = "Terminating"
)

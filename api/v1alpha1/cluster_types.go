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
)

// Phase is a custom type representing the phase of a cluster.
type Phase string

// Constants representing the phases of an instance lifecycle.
const (
	Pending     Phase = "Pending"
	Progressing Phase = "Progressing"
	Ready       Phase = "Ready"
	Failed      Phase = "Failed"
	Terminating Phase = "Terminating"
	Unknown     Phase = "Unknown"
)

type ClusterConditionType string

const (
	ClusterReady ClusterConditionType = "Ready"
	KindReady    ClusterConditionType = "KindReady"
	MetalLBReady ClusterConditionType = "MetalLBReady"
)

// ClusterSpec defines the desired state of Cluster.
type ClusterSpec struct{}

// ClusterStatus defines the observed state of Cluster.
type ClusterStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +kubebuilder:default=Unknown
	Phase Phase `json:"phase"`
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

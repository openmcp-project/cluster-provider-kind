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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AccessRequestSpec defines the desired state of AccessRequest.
type AccessRequestSpec struct {
	ClusterRef corev1.LocalObjectReference `json:"clusterRef,omitempty"`
	Rules      []rbacv1.PolicyRule         `json:"rules"`
}

// AccessRequestStatus defines the observed state of AccessRequest.
type AccessRequestStatus struct {
	Kubeconfig *Kubeconfig `json:"kubeconfig,omitempty"`
}

// Kubeconfig contains the information needed to access a cluster.
type Kubeconfig struct {
	SecretRef      corev1.SecretReference `json:"secretRef,omitempty"`
	ExpiresAt      metav1.Time            `json:"expiresAt,omitempty"`
	ServiceAccount ServiceAccountRef      `json:"serviceAccount,omitempty"`
}

// ServiceAccountRef contains the information needed to access a service account.
type ServiceAccountRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AccessRequest is the Schema for the accessrequests API.
type AccessRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccessRequestSpec   `json:"spec,omitempty"`
	Status AccessRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessRequestList contains a list of AccessRequest.
type AccessRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccessRequest{}, &AccessRequestList{})
}

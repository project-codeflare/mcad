/*
Copyright 2023 IBM Corporation.

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

package v1beta1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterInfoSpec defines the desired state of ClusterInfo
type ClusterInfoSpec struct {
}

// ClusterInfoStatus defines the observed state of ClusterInfo
type ClusterInfoStatus struct {
	// Capacity available on the cluster
	Capacity v1.ResourceList `json:"capacity,omitempty"`

	// When last updated
	Time metav1.Time `json:"time,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterInfo is the Schema for the clusterinfoes API
type ClusterInfo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterInfoSpec   `json:"spec,omitempty"`
	Status ClusterInfoStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterInfoList contains a list of ClusterInfo
type ClusterInfoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterInfo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterInfo{}, &ClusterInfoList{})
}

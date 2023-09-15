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

// ClusterCapacitySpec defines the desired state of ClusterCapacity
type ClusterCapacitySpec struct {
}

// ClusterCapacityStatus defines the observed state of ClusterCapacity
type ClusterCapacityStatus struct {
	// Capacity available on the cluster
	Capacity v1.ResourceList `json:"capacity"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterCapacity is the Schema for the clustercapacities API
type ClusterCapacity struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterCapacitySpec   `json:"spec,omitempty"`
	Status ClusterCapacityStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterCapacityList contains a list of ClusterCapacity
type ClusterCapacityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterCapacity `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterCapacity{}, &ClusterCapacityList{})
}

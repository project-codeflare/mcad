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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// BoxedJobSpec defines the desired state of a BoxedJob object
type BoxedJobSpec struct {
	// Components lists the components in the job
	Components []BoxedJobComponent `json:"components"`

	// Suspend suspends the job when set to true
	Suspend bool `json:"suspend,omitempty"`
}

// BoxedJobComponent describes a component of the job
type BoxedJobComponent struct {
	// Topoloy of the component
	Topology []BoxedJobPodSet `json:"topology,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:EmbeddedResource
	// Spec of the component
	Spec runtime.RawExtension `json:"spec,omitempty"`
}

// BoxedJobPodSet describes an homogeneous set of pods
type BoxedJobPodSet struct {
	// Count is the number of pods in the set
	Count int32 `json:"count"`

	// Requests per pod
	Requests v1.ResourceList `json:"requests,omitempty"`

	// Limits per pod
	Limits v1.ResourceList `json:"limits,omitempty"`
}

// BoxedJobStatus defines the observed state of the BoxedJob object
type BoxedJobStatus struct {
	// Phase of the BoxedJob object: Empty, Suspended, Deploying, Running, Suspending, Deleting, Completed, Failed
	Phase string `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.phase`

// BoxedJob is the Schema for the boxedjobs API
type BoxedJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BoxedJobSpec   `json:"spec,omitempty"`
	Status BoxedJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BoxedJobList contains a list of BoxedJob
type BoxedJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BoxedJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BoxedJob{}, &BoxedJobList{})
}

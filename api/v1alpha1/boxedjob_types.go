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
	// PodSets contained in the component
	PodSets []BoxedJobPodSet `json:"podSets"`

	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:EmbeddedResource
	// Template for the component
	Template runtime.RawExtension `json:"template"`
}

// BoxedJobPodSet describes an homogeneous set of pods
type BoxedJobPodSet struct {
	// Replicas is the number of pods in the set
	Replicas *int32 `json:"replicas,omitempty"`

	// Requests per pod
	Path string `json:"path"`
}

// BoxedJobStatus defines the observed state of the BoxedJob object
type BoxedJobStatus struct {
	// Phase of the BoxedJob object
	Phase BoxedJobPhase `json:"phase,omitempty"`
}

// BoxedJobPhase is the phase of the BoxedJob object
type BoxedJobPhase string

const (
	BoxedJobEmpty      BoxedJobPhase = ""
	BoxedJobSuspended  BoxedJobPhase = "Suspended"
	BoxedJobDeploying  BoxedJobPhase = "Deploying"
	BoxedJobRunning    BoxedJobPhase = "Running"
	BoxedJobSuspending BoxedJobPhase = "Suspending"
	BoxedJobDeleting   BoxedJobPhase = "Deleting"
	BoxedJobCompleted  BoxedJobPhase = "Completed"
	BoxedJobFailed     BoxedJobPhase = "Failed"
)

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

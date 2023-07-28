/*
Copyright 2023.

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

// AppWrapperSpec defines the desired state of AppWrapper
type AppWrapperSpec struct {
	// Priority
	Priority int32 `json:"priority,omitempty"`

	// Minimum number of pods that need to run and succeed
	MinPods int32 `json:"minPods,omitempty"`

	// Max requeuings
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// Wrapped resources
	Resources []AppWrapperResource `json:"resources"`
}

// AppWrapperStatus defines the observed state of AppWrapper
type AppWrapperStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// When last dispatched
	LastDispatchTime metav1.Time `json:"lastDispatchTime,omitempty"`

	// When last requeued
	LastRequeuingTime metav1.Time `json:"lastRequeuingTime,omitempty"`

	// How many times requeued
	Requeued int32 `json:"requeued,omitempty"`

	// Conditions
	Conditions []AppWrapperCondition `json:"conditions,omitempty"`
}

// AppWrapperPhase is the label for the AppWrapper status
type AppWrapperPhase string

// AppWrapper phases
const (
	// Queued: resource requests ARE NOT reserved
	// Decide to dispatch -> Dispatching
	Queued AppWrapperPhase = "Queued"

	// Dispatching: resource requests ARE reserved (resource creation in progress)
	// Create wrapped resources -> Running or Failed (parsing error) or Failed/Requeuing (timeout creating resources)
	Dispatching AppWrapperPhase = "Dispatching"

	// Running: resource requests ARE reserved
	// Monitor pods -> Succeeded or Failed/Requeuing (pod failed or min pod running/non-running pod timeout)
	Running AppWrapperPhase = "Running"

	// Succeeded: resource requests ARE NOT reserved
	// Entering when >= max(min pods, 1) succeeded and no other pod
	Succeeded AppWrapperPhase = "Succeeded"

	// Failed: resource requests ARE reserved (because failure can be partial and there is no cleanup)
	// Entering error if requeued status >= max retries spec
	Failed AppWrapperPhase = "Failed"

	// Terminating: resource requests ARE reserved (wrapped resource deletion in progress)
	// Entering when deletion requested, until wrapped resources are deleted or timeout
	Terminating AppWrapperPhase = "Terminating"

	// Requeuing: resource requests ARE reserved (wrapped resource deletion in progress)
	// Entering on error if requeued status < max retries spec (except always entering Failed on parsing error)
	Requeuing AppWrapperPhase = "Requeuing"
)

// AppWrapperResource is the schema for the wrapped resources
type AppWrapperResource struct {
	// Replica count
	Replicas int32 `json:"replicas"`

	// Resource requests per replica
	Requests v1.ResourceList `json:"requests"`

	// Resource template
	Template runtime.RawExtension `json:"template"`
}

// AppWrapper condition
type AppWrapperCondition struct {
	// Timestamp
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Condition
	Reason string `json:"reason"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="STATUS",type="string",JSONPath=`.status.phase`

// AppWrapper object
type AppWrapper struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppWrapperSpec   `json:"spec"`
	Status AppWrapperStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AppWrapperList contains a list of AppWrapper
type AppWrapperList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppWrapper `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppWrapper{}, &AppWrapperList{})
}

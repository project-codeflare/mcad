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
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// AppWrapperSpec defines the desired state of AppWrapper
type AppWrapperSpec struct {
	// Priority
	Priority int32 `json:"priority,omitempty"`

	// Minimum number of pods that need to run and succeed
	// These pods have to be labeled with the AppWrapper name to be accounted for and monitored by mcad:
	//   mcad.codeflare.dev: <appWrapper-name>
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
// There are also two pseudo phases:
// - object creation: empty "" phase
// - object deletion: any phase with deletion timestamp set
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
	Succeeded AppWrapperPhase = "Succeeded"

	// Failed: resource requests ARE reserved (because failure can be partial and there is no cleanup)
	// Entered on:
	// - parsing error (always)
	// - Requeuing timeout deleting resources (always)
	// - Dispatching/Running errors and timeouts (after max retries)
	Failed AppWrapperPhase = "Failed"

	// Requeuing: resource requests ARE reserved (wrapped resource deletion in progress)
	// Try deleting resources -> Queued or Failed if timeout deleting resources
	// Entered on Dispatching/Running errors and timeouts (before max retries)
	Requeuing AppWrapperPhase = "Requeuing"

	// Deleted: resources ARE NOT reserved
	Deleted AppWrapperPhase = "Deleted"
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

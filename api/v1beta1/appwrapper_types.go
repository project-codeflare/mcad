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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// AppWrapperSpec defines the desired state of AppWrapper
type AppWrapperSpec struct {
	// Priority
	Priority int32 `json:"priority,omitempty"`

	// Priority slope
	DoNotUsePrioritySlope resource.Quantity `json:"priorityslope,omitempty"`

	// Scheduling specification
	Scheduling SchedulingSpec `json:"schedulingSpec,omitempty"`

	// Wrapped resources
	Resources AppWrapperResources `json:"resources"`

	// Dispatcher status
	DispatcherStatus AppWrapperDispatcherStatus `json:"dispatcherStatus,omitempty"`

	// Sustainable specification
	Sustainable SustainableSpec `json:"sustainable,omitempty"`

	// Dispatching gates
	DispatchingGates []DispatchingGate `json:"dispatchingGates,omitempty"`
}

// Dispatching gate
type DispatchingGate string

type SchedulingSpec struct {
	// Minimum number of pods that need to run and succeed
	// These pods have to be labeled with the AppWrapper name to be accounted for and monitored by mcad:
	//   workload.codeflare.dev: <appWrapper-name>
	MinAvailable int32 `json:"minAvailable,omitempty"`

	// Requeuing specification
	Requeuing RequeuingSpec `json:"requeuing,omitempty"`

	// Cluster specification
	ClusterScheduling *ClusterSchedulingSpec `json:"clusterScheduling,omitempty"`
}

type RequeuingSpec struct {
	// Max requeuings permitted
	MaxNumRequeuings int32 `json:"maxNumRequeuings,omitempty"`
}

// Where to run
type ClusterSchedulingSpec struct {
	PolicyResult ClusterDecision `json:"policyResult,omitempty"`
}

type ClusterDecision struct {
	TargetCluster ClusterReference `json:"targetCluster,omitempty"`
}

type ClusterReference struct {
	Name string `json:"name"`
}

type SustainableSpec struct {
	// TODO
}

// Status from the dispatcher perspective
type AppWrapperDispatcherStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// When last dispatched
	LastDispatchingTime metav1.Time `json:"lastDispatchingTime,omitempty"`

	// How many times requeued
	Requeued int32 `json:"requeued,omitempty"`

	// Transition log
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`

	// Total time dispatched (excluding current leg)
	DispatchedNanos int64 `json:"dispatchedNanos,omitempty"`
}

// Status from the runner perspective
type AppWrapperRunnerStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// When last running
	LastRunningTime metav1.Time `json:"lastRunningTime,omitempty"`

	// When last requeued
	LastRequeuingTime metav1.Time `json:"lastRequeuingTime,omitempty"`

	// Transition log
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`
}

// Status
type AppWrapperStatus struct {
	// Runner status
	RunnerStatus AppWrapperRunnerStatus `json:"runnerStatus,omitempty"`
}

// AppWrapperPhase is the label for the AppWrapper status
type AppWrapperPhase string

const (
	// no resource reservation
	Empty AppWrapperPhase = ""

	// no resource reservation
	Queued AppWrapperPhase = "Queued" // dispatcher-only phase

	// resources are reserved
	Dispatching AppWrapperPhase = "Dispatching"

	// resources are reserved
	Running AppWrapperPhase = "Running"

	// no resource reservation even if pods may still exist in completed state
	Succeeded AppWrapperPhase = "Succeeded"

	// resources are reserved as errors may be partial
	// AppWrapper may be requeued
	Errored AppWrapperPhase = "Errored" // runner-only phase

	// resources are reserved as failures may be partial
	// AppWrapper may not be requeued
	Failed AppWrapperPhase = "Failed"

	// resources are reserved
	Requeuing AppWrapperPhase = "Requeuing"
)

// AppWrapperResource
type AppWrapperResources struct {
	// GenericItems
	GenericItems []GenericItem `json:"GenericItems"`
}

// GenericItems is the schema for the wrapped resources
type GenericItem struct {
	DoNotUseReplicas int32 `json:"replicas,omitempty"`

	// Replica count and resource requests
	CustomPodResources []CustomPodResource `json:"custompodresources,omitempty"`

	// A comma-separated list of keywords to match against condition types
	CompletionStatus string `json:"completionstatus,omitempty"`

	// Resource template
	GenericTemplate runtime.RawExtension `json:"generictemplate"`
}

// Replica count and resource requests
type CustomPodResource struct {
	// Replica count
	Replicas int32 `json:"replicas"`

	// Resource requests per replica
	Requests v1.ResourceList `json:"requests"`

	// Limits per replica
	DoNotUseLimits v1.ResourceList `json:"limits,omitempty"`
}

// Phase transition
type AppWrapperTransition struct {
	// Timestamp
	Time metav1.Time `json:"time"`

	// Reason
	Reason string `json:"reason,omitempty"`

	// Phase
	Phase AppWrapperPhase `json:"phase"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="DISPATCHER",type="string",JSONPath=`.spec.dispatcherStatus.phase`
//+kubebuilder:printcolumn:name="RUNNER",type="string",JSONPath=`.status.runnerStatus.phase`

// AppWrapper object
type AppWrapper struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AppWrapperSpec `json:"spec"`

	// AppWrapper status
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

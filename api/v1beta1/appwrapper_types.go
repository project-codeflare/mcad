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

	// Scheduling specification
	Scheduling SchedulingSpec `json:"schedulingSpec,omitempty"`

	// Wrapped resources
	Resources AppWrapperResources `json:"resources"`

	// Dispatcher status
	DispatcherStatus AppWrapperDispatcherStatus `json:"dispatcherStatus,omitempty"`
}

type SchedulingSpec struct {
	// Minimum number of pods that need to run and succeed
	// These pods have to be labeled with the AppWrapper name to be accounted for and monitored by mcad:
	//   workload.codeflare.dev: <appWrapper-name>
	MinAvailable int32 `json:"minAvailable,omitempty"`

	// Requeuing specification
	Requeuing RequeuingSpec `json:"requeuing,omitempty"`

	// Cluster specification
	ClusterScheduling ClusterSchedulingSpec `json:"clusterScheduling,omitempty"`
}

type RequeuingSpec struct {
	// Max requeuings
	MaxNumRequeuings int32 `json:"maxNumRequeuings,omitempty"`
}

type ClusterSchedulingSpec struct {
	PolicyResult ClusterDecision `json:"policyResult,omitempty"`
}

type ClusterDecision struct {
	TargetCluster ClusterReference `json:"targetCluster,omitempty"`
}

type ClusterReference struct {
	Name string `json:"name"`
}

type AppWrapperDispatcherStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// How many times requeued
	Requeued int32 `json:"requeued,omitempty"`

	// Transitions
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`
}

// AppWrapperStatus defines the observed state of AppWrapper
type AppWrapperStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// Transitions
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`
}

type AppWrapperPhase string

const (
	Empty       AppWrapperPhase = ""
	Queued      AppWrapperPhase = "Queued"
	Dispatching AppWrapperPhase = "Dispatching"
	Running     AppWrapperPhase = "Running"
	Succeeded   AppWrapperPhase = "Succeeded"
	Errored     AppWrapperPhase = "Errored"   // runner-only, can be requeued
	Failed      AppWrapperPhase = "Failed"    // cannot be requeued
	Requeuing   AppWrapperPhase = "Requeuing" // dispatcher only
)

// AppWrapperResource
type AppWrapperResources struct {
	// GenericItems
	GenericItems []GenericItem `json:"GenericItems"`
}

// GenericItems is the schema for the wrapped resources
type GenericItem struct {
	// CustomPodResources
	CustomPodResources []CustomPodResource `json:"custompodresources,omitempty"`

	// Resource template
	GenericTemplate runtime.RawExtension `json:"generictemplate"`
}

type CustomPodResource struct {
	// Replica count
	Replicas int32 `json:"replicas"`

	// Resource requests per replica
	Requests v1.ResourceList `json:"requests"`
}

// AppWrapper transition
type AppWrapperTransition struct {
	// Timestamp
	Time metav1.Time `json:"time"`

	// Phase
	Phase AppWrapperPhase `json:"phase"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="DISPATCHER",type="string",JSONPath=`.spec.dispatcherStatus.phase`
//+kubebuilder:printcolumn:name="RUNNER",type="string",JSONPath=`.status.phase`

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

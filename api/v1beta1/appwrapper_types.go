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
	"k8s.io/apimachinery/pkg/runtime"
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
}

type SchedulingSpec struct {
	// Minimum number of pods
	MinAvailable int32 `json:"minAvailable,omitempty"`

	// Requeuing specification
	Requeuing RequeuingSpec `json:"requeuing,omitempty"`
}

type RequeuingSpec struct {
	// Max requeuings permitted
	MaxNumRequeuings int32 `json:"maxNumRequeuings,omitempty"`
}

// AppWrapperStatus defines the observed state of AppWrapper
type AppWrapperStatus struct {
	// Phase
	Phase AppWrapperPhase `json:"phase,omitempty"`

	// When last dispatched
	DispatchTimestamp metav1.Time `json:"dispatchTimestamp,omitempty"`

	// When last requeued
	RequeueTimestamp metav1.Time `json:"requeueTimestamp,omitempty"`

	// How many times restarted
	Restarts int32 `json:"restarts"`

	// Transition log
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`
}

// AppWrapperPhase is the label for the AppWrapper status
type AppWrapperPhase string

const (
	// no resource reservation
	Empty AppWrapperPhase = ""

	// no resource reservation
	Queued AppWrapperPhase = "Queued"

	// resources are reserved
	Dispatching AppWrapperPhase = "Dispatching"

	// resources are reserved
	Running AppWrapperPhase = "Running"

	// no resource reservation
	Succeeded AppWrapperPhase = "Succeeded"

	// resources are reserved (some pods may still be running)
	Failed AppWrapperPhase = "Failed"

	// resources are reserved (some pods may still be running)
	Requeuing AppWrapperPhase = "Requeuing"
)

// AppWrapper resources
type AppWrapperResources struct {
	// Array of GenericItems
	GenericItems []GenericItem `json:"GenericItems"`
}

// AppWrapper resource
type GenericItem struct {
	DoNotUseReplicas int32 `json:"replicas,omitempty"`

	// Array of resource requests
	CustomPodResources []CustomPodResource `json:"custompodresources,omitempty"`

	// A comma-separated list of keywords to match against condition types
	CompletionStatus string `json:"completionstatus,omitempty"`

	// Resource template
	GenericTemplate runtime.RawExtension `json:"generictemplate"`
}

// Resource requests
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

	// Phase entered
	Phase AppWrapperPhase `json:"phase"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Restarts",type="integer",JSONPath=`.status.restarts`
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppWrapper is the Schema for the appwrappers API
type AppWrapper struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppWrapperSpec   `json:"spec,omitempty"`
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

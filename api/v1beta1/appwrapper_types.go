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
	NotImplemented_PrioritySlope resource.Quantity `json:"priorityslope,omitempty"`

	NotImplemented_Service AppWrapperService `json:"service,omitempty"`

	// Wrapped resources
	Resources AppWrapperResources `json:"resources"`

	NotImplemented_Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Scheduling specifies the parameters used for scheduling the wrapped resources.
	// It defines the policy for requeuing jobs based on the number of running pods.
	Scheduling SchedulingSpec `json:"schedulingSpec,omitempty"`
}

type SchedulingSpec struct {
	NotImplemented_NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Minimum number of expected running and successful pods.
	// Set to -1 to disable pod monitoring, cleanup on failure, and termination detection based on pod counts.
	// Set to 0 to enable pod monitoring (at least 1 pod), cleanup on failure, and disable termination detection.
	// Set to n>=1 to enable pod monitoring (at least n pods), cleanup on failure, and termination detection.
	MinAvailable int32 `json:"minAvailable,omitempty"`

	// Requeuing specification
	Requeuing RequeuingSpec `json:"requeuing,omitempty"`

	NotImplemented_DispatchDuration NotImplemented_DispatchDurationSpec `json:"dispatchDuration,omitempty"`
}

type NotImplemented_DispatchDurationSpec struct {
	Expected int32 `json:"expected,omitempty"`
	Limit    int32 `json:"limit,omitempty"`
	Overrun  bool  `json:"overrun,omitempty"`
}

type RequeuingSpec struct {
	NotImplemented_InitialTimeInSeconds int64 `json:"initialTimeInSeconds,omitempty"`

	// Initial waiting time before requeuing conditions are checked
	// +kubebuilder:default=270
	TimeInSeconds int64 `json:"timeInSeconds,omitempty"`

	// +kubebuilder:default=0
	NotImplemented_MaxTimeInSeconds int64 `json:"maxTimeInSeconds,omitempty"`

	// +kubebuilder:default=exponential
	NotImplemented_GrowthType string `json:"growthType,omitempty"`

	// +kubebuilder:default=0
	NotImplemented_NumRequeuings int32 `json:"numRequeuings,omitempty"`

	// Max requeuings permitted (infinite if zero)
	// +kubebuilder:default=0
	MaxNumRequeuings int32 `json:"maxNumRequeuings,omitempty"`

	// Enable forced deletion after delay if greater than zero
	// +kubebuilder:default=570
	ForceDeletionTimeInSeconds int64 `json:"forceDeletionTimeInSeconds,omitempty"`

	// Waiting time before trying to dispatch again after requeuing
	// +kubebuilder:default=90
	PauseTimeInSeconds int64 `json:"pauseTimeInSeconds,omitempty"`
}

// AppWrapperStatus defines the observed state of AppWrapper
type AppWrapperStatus struct {
	// State
	State AppWrapperState `json:"state,omitempty"`

	// Status of wrapped resources
	Step AppWrapperStep `json:"step,omitempty"`

	// When last dispatched
	DispatchTimestamp metav1.Time `json:"dispatchTimestamp,omitempty"`

	// When last requeued
	RequeueTimestamp metav1.Time `json:"requeueTimestamp,omitempty"`

	// How many times restarted
	Restarts int32 `json:"restarts"`

	// Transition log
	Transitions []AppWrapperTransition `json:"transitions,omitempty"`

	// Number of transitions
	TransitionCount int32 `json:"transitionCount,omitempty"`

	// Conditions
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AppWrapperState is the label for the AppWrapper status
type AppWrapperState string

// AppWrapperState is the status of wrapped resources
type AppWrapperStep string

// AppWrapperQueuedReason is the Type for the Queued Condition
type AppWrapperQueuedReason string

const (
	// Initial state upon creation of the AppWrapper object
	Empty AppWrapperState = ""

	// AppWrapper has not been dispatched yet or has been requeued
	Queued AppWrapperState = "Pending"

	// AppWrapper has been dispatched and not requeued
	Running AppWrapperState = "Running"

	// AppWrapper completed successfully
	Succeeded AppWrapperState = "Completed"

	// AppWrapper failed and is not requeued
	Failed AppWrapperState = "Failed"

	// Resources are not deployed
	Idle AppWrapperStep = ""

	// The MCAD dispatcher is dispatching an appwrapper for execution by the runner
	Dispatching AppWrapperStep = "dispatching"

	// The MCAD runner has accepted an appwrapper for execution
	Accepting AppWrapperStep = "accepting"

	// The MCAD runner is in the process of creating the wrapped resources
	Creating AppWrapperStep = "creating"

	// The wrapped resources have been deployed successfully
	Created AppWrapperStep = "created"

	// The MCAD runner is in the process of deleting the wrapped resources
	Deleting AppWrapperStep = "deleting"

	// The MCAD runner has returned control of an appwrapper to the dispatcher
	Deleted AppWrapperStep = "deleted"

	// Queued because of insufficient available resources
	QueuedInsufficientResources AppWrapperQueuedReason = "InsufficientResources"

	// Queued because of insufficient available quota
	QueuedInsufficientQuota AppWrapperQueuedReason = "InsufficientQuota"

	// Queued because it was requeued
	QueuedRequeue AppWrapperQueuedReason = "Requeued"

	// Not Queued because it was dispatched
	QueuedDispatch AppWrapperQueuedReason = "Dispatched"
)

// AppWrapperService
type AppWrapperService struct {
	Spec v1.ServiceSpec `json:"spec"`
}

// AppWrapper resources
type AppWrapperResources struct {
	GenericItems []GenericItem `json:"GenericItems,omitempty"`
}

// AppWrapper resource
type GenericItem struct {
	NotImplemented_Replicas int32 `json:"replicas,omitempty"`

	NotImplemented_MinAvailable *int32 `json:"minavailable,omitempty"`

	NotImplemented_Allocated int32 `json:"allocated,omitempty"`

	NotImplemented_Priority int32 `json:"priority,omitempty"`

	NotImplemented_PrioritySlope resource.Quantity `json:"priorityslope,omitempty"`

	// The template for the resource
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:EmbeddedResource
	GenericTemplate runtime.RawExtension `json:"generictemplate,omitempty"`

	// Array of resource requests
	CustomPodResources []CustomPodResource `json:"custompodresources,omitempty"`

	// A comma-separated list of keywords to match against condition types
	CompletionStatus string `json:"completionstatus,omitempty"`
}

// Resource requests
type CustomPodResource struct {
	// Replica count
	Replicas int32 `json:"replicas"`

	// Resource requests per replica
	Requests v1.ResourceList `json:"requests"`

	// Limits per replica
	NotImplemented_Limits v1.ResourceList `json:"limits,omitempty"`
}

// State transition
type AppWrapperTransition struct {
	// Timestamp
	Time metav1.Time `json:"time"`

	// Reason
	Reason string `json:"reason,omitempty"`

	// State entered
	State AppWrapperState `json:"state"`

	// Status of wrapped resources
	Step AppWrapperStep `json:"step,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.state`
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

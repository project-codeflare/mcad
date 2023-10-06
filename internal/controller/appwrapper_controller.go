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

package controller

import (
	"time"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// The super type of Dispatcher and Runner reconcilers
type AppWrapperReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Cache  map[types.UID]*CachedAppWrapper // cache appWrapper updates for write/read consistency
}

const (
	nameLabel          = "workload.codeflare.dev"           // owner name label for wrapped resources
	namespaceLabel     = "workload.codeflare.dev/namespace" // owner namespace label for wrapped resources
	finalizer          = "workload.codeflare.dev/finalizer" // finalizer name
	nvidiaGpu          = "nvidia.com/gpu"                   // GPU resource name
	specNodeName       = ".spec.nodeName"                   // key to index pods based on node placement
	DefaultClusterName = "self"                             // default cluster name
)

// PodCounts summarize the status of the pods associated with one AppWrapper
type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

// We cache AppWrapper phases because the reconciler cache does not immediately reflect updates.
// A Get or List call soon after an Update or Status.Update call may not reflect the latest object.
// See https://github.com/kubernetes-sigs/controller-runtime/issues/1622.
// We use the number of transitions to confirm our cached version is more recent than the reconciler cache.
// When reconciling an AppWrapper, we proactively detect and abort on conflicts.
// To defend against bugs in the cache implementation and egregious AppWrapper edits,
// we eventually give up on persistent conflicts and remove the AppWrapper phase from the cache.

// TODO garbage collection

// Cached AppWrapper
type CachedAppWrapper struct {
	// AppWrapper phase
	Phase mcadv1beta1.AppWrapperPhase

	// Number of transitions
	Transitions int

	// First conflict detected between reconciler cache and our cache if not nil
	Conflict *time.Time
}

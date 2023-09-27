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

// Dispatcher reconciles an AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Cache  map[types.UID]*CachedAppWrapper // cache appWrapper updates for write/read consistency
}

const (
	nameLabel      = "workload.codeflare.dev"           // owner name label for wrapped resources
	namespaceLabel = "workload.codeflare.dev/namespace" // owner namespace label for wrapped resources
	finalizer      = "workload.codeflare.dev/finalizer" // finalizer name
	nvidiaGpu      = "nvidia.com/gpu"                   // GPU resource name
	specNodeName   = ".spec.nodeName"                   // key to index pods based on node placement
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
// Therefore we need to maintain our own cache to make sure new dispatching decisions accurately account
// for recent dispatching decisions. The cache is populated on phase updates.
// The cache is only meant to be used for AppWrapper List calls when computing available resources.
// We use the number of transitions to confirm our cached version is more recent than the reconciler cache.
// We remove cache entries when removing finalizers.
// When reconciling an AppWrapper, we proactively detect and abort on conflicts as
// there is no point working on a stale AppWrapper. We know etcd updates will fail.
// This conflict detection reduces the probability of an etcd update failure but does not eliminate it.
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

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
	"context"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// Compute resources reserved by AppWrappers at every priority level for the specified cluster
// Sort queued AppWrappers in dispatch order
// AppWrappers in output queue must be cloned if mutated
func (r *Dispatcher) listAppWrappers(ctx context.Context, cluster string) (map[int]Weights, []*mcadv1beta1.AppWrapper, error) {
	appWrappers := &mcadv1beta1.AppWrapperList{}
	if err := r.List(ctx, appWrappers, client.UnsafeDisableDeepCopy); err != nil {
		return nil, nil, err
	}
	requests := map[int]Weights{}        // total request per priority level
	queue := []*mcadv1beta1.AppWrapper{} // queued appWrappers
	for _, appWrapper := range appWrappers.Items {
		if appWrapper.Spec.Scheduling.ClusterScheduling != nil &&
			appWrapper.Spec.Scheduling.ClusterScheduling.PolicyResult.TargetCluster.Name != cluster ||
			cluster != DefaultClusterName {
			continue // skip AppWrappers targeting other clusters or no cluster
		}
		// get phase from cache if available as reconciler cache may be lagging
		phase := r.getCachedPhase(&appWrapper)
		// make sure to initialize weights for every known priority level
		if requests[int(appWrapper.Spec.Priority)] == nil {
			requests[int(appWrapper.Spec.Priority)] = Weights{}
		}
		if isActivePhase(phase) {
			// discount resource requested by AppWrapper
			awRequest := aggregateRequests(&appWrapper)
			requests[int(appWrapper.Spec.Priority)].Add(awRequest)
		} else if phase == mcadv1beta1.Queued && len(appWrapper.Spec.DispatchingGates) == 0 {
			// add AppWrapper to queue
			copy := appWrapper // must copy appWrapper before taking a reference, shallow copy ok
			queue = append(queue, &copy)
		}
	}
	// propagate reservations at all priority levels to all levels below
	assertPriorities(requests)
	// order AppWrapper queue based on priority and creation time
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Spec.Priority > queue[j].Spec.Priority {
			return true
		}
		if queue[i].Spec.Priority < queue[j].Spec.Priority {
			return false
		}
		return queue[i].CreationTimestamp.Before(&queue[j].CreationTimestamp)
	})
	return requests, queue, nil
}

// Find next AppWrapper to dispatch in queue order
func (r *Dispatcher) selectForDispatch(ctx context.Context) (*mcadv1beta1.AppWrapper, error) {
	// iterate over clusters
	clusters := &mcadv1beta1.ClusterInfoList{}
	if err := r.List(ctx, clusters, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	for _, cluster := range clusters.Items {
		capacity := NewWeights(cluster.Status.Capacity)
		requests, queue, err := r.listAppWrappers(ctx, cluster.Name)
		if err != nil {
			return nil, err
		}
		// compute available cluster capacity at each priority level
		// available cluster capacity = total capacity reported in cluster info - capacity reserved by AppWrappers
		available := map[int]Weights{}
		for priority, request := range requests {
			// copy capacity before subtracting request
			available[priority] = Weights{}
			available[priority].Add(capacity)
			available[priority].Sub(request)
		}
		// return first AppWrapper that fits if any
		for _, appWrapper := range queue {
			request := aggregateRequests(appWrapper)
			if request.Fits(available[int(appWrapper.Spec.Priority)]) {
				return appWrapper.DeepCopy(), nil // deep copy appWrapper
			}
		}
	}
	// no queued AppWrapper fits
	return nil, nil
}

// Aggregated request by AppWrapper
func aggregateRequests(appWrapper *mcadv1beta1.AppWrapper) Weights {
	request := Weights{}
	for _, r := range appWrapper.Spec.Resources.GenericItems {
		for _, cpr := range r.CustomPodResources {
			request.AddProd(cpr.Replicas, NewWeights(cpr.Requests))
		}
	}
	return request
}

// Propagate reservations at all priority levels to all levels below
func assertPriorities(w map[int]Weights) {
	keys := make([]int, len(w))
	i := 0
	for k := range w {
		keys[i] = k
		i += 1
	}
	sort.Ints(keys)
	for i := len(keys) - 1; i > 0; i-- {
		w[keys[i-1]].Add(w[keys[i]])
	}
}

// Are resources reserved in this phase
func isActivePhase(phase mcadv1beta1.AppWrapperPhase) bool {
	switch phase {
	case mcadv1beta1.Dispatching, mcadv1beta1.Running, mcadv1beta1.Failed, mcadv1beta1.Requeuing:
		return true
	default:
		return false // Queued, Empty, Succeeded
	}
}

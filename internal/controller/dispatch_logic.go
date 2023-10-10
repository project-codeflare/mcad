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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// Compute available cluster capacity
func (r *AppWrapperReconciler) computeCapacity(ctx context.Context) (Weights, error) {
	// return cached capacity if unexpired
	if !time.Now().After(r.NextSync) {
		return r.ClusterCapacity, nil
	}
	capacity := Weights{}
	// add allocatable capacity for each schedulable node
	nodes := &v1.NodeList{}
	if err := r.List(ctx, nodes, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	for _, node := range nodes.Items {
		// skip unschedulable nodes
		if node.Spec.Unschedulable {
			continue
		}
		// add allocatable capacity on the node
		capacity.Add(NewWeights(node.Status.Allocatable))
		// subtract requests from non-AppWrapper, non-terminated pods on this node
		fieldSelector, err := fields.ParseSelector(specNodeName + "=" + node.Name)
		if err != nil {
			return nil, err
		}
		pods := &v1.PodList{}
		if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
			client.MatchingFieldsSelector{Selector: fieldSelector}); err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			if _, ok := pod.GetLabels()[nameLabel]; !ok && pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
				for _, container := range pod.Spec.Containers {
					capacity.Sub(NewWeights(container.Resources.Requests))
				}
			}
		}
	}
	r.ClusterCapacity = capacity
	r.NextSync = time.Now().Add(clusterInfoTimeout)
	return capacity, nil
}

// Compute resources reserved by AppWrappers at every priority level for the specified cluster
// Sort queued AppWrappers in dispatch order
// AppWrappers in output queue must be cloned if mutated
func (r *AppWrapperReconciler) listAppWrappers(ctx context.Context) (map[int]Weights, []*mcadv1beta1.AppWrapper, error) {
	appWrappers := &mcadv1beta1.AppWrapperList{}
	if err := r.List(ctx, appWrappers, client.UnsafeDisableDeepCopy); err != nil {
		return nil, nil, err
	}
	requests := map[int]Weights{}        // total request per priority level
	queue := []*mcadv1beta1.AppWrapper{} // queued appWrappers
	for _, appWrapper := range appWrappers.Items {
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
		} else if phase == mcadv1beta1.Queued {
			// add AppWrapper to queue
			copy := appWrapper // must copy appWrapper before taking a reference, shallow copy ok
			queue = append(queue, &copy)
		}
	}
	// propagate reservations at all priority levels to all levels below
	assertPriorities(requests)
	// order AppWrapper queue based on priority and precedence (creation time)
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
func (r *AppWrapperReconciler) selectForDispatch(ctx context.Context) (*mcadv1beta1.AppWrapper, error) {
	capacity, err := r.computeCapacity(ctx)
	if err != nil {
		return nil, err
	}
	requests, queue, err := r.listAppWrappers(ctx)
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
			return appWrapper.DeepCopy(), nil // deep copy AppWrapper
		}
	}
	// no queued AppWrapper fits
	return nil, nil
}

// Aggregate requests
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
		return false // Empty, Queued, Succeeded
	}
}

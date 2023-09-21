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

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// Compute resources reserved by AppWrappers at every priority level
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
		// get phase from cache if available
		phase := r.getCachedPhase(&appWrapper)
		// make sure to initialize weights for every known priority level
		if requests[int(appWrapper.Spec.Priority)] == nil {
			requests[int(appWrapper.Spec.Priority)] = Weights{}
		}
		if isActivePhase(phase) {
			// use max request among appWrapper request and total request of non-terminated appWrapper pods
			// TODO pod metrics are not available in dispatcher mode
			awRequest := aggregateRequests(&appWrapper)
			podRequest := Weights{}
			pods := &v1.PodList{}
			if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
				client.MatchingLabels{namespaceLabel: appWrapper.Namespace, nameLabel: appWrapper.Name}); err != nil {
				return nil, nil, err
			}
			for _, pod := range pods.Items {
				if pod.Spec.NodeName != "" && pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
					for _, container := range pod.Spec.Containers {
						podRequest.Add(NewWeights(container.Resources.Requests))
					}
				}
			}
			// compute max
			awRequest.Max(podRequest)
			requests[int(appWrapper.Spec.Priority)].Add(awRequest)
		} else if phase == mcadv1beta1.Queued {
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

// Find next AppWrapper to dispatch in queue order, return true AppWrapper is last in queue
// TODO handle more than one cluster
func (r *AppWrapperReconciler) selectForDispatch(ctx context.Context) (*mcadv1beta1.AppWrapper, bool, error) {
	clusters := &mcadv1beta1.ClusterInfoList{}
	if err := r.List(ctx, clusters, client.UnsafeDisableDeepCopy); err != nil {
		return nil, false, err
	}
	if len(clusters.Items) == 0 {
		return nil, false, nil // TODO no cluster available
	}
	capacity := NewWeights(clusters.Items[0].Status.Capacity)
	requests, queue, err := r.listAppWrappers(ctx)
	if err != nil {
		return nil, false, err
	}
	available := map[int]Weights{}
	for priority, request := range requests {
		available[priority] = Weights{}
		available[priority].Add(capacity)
		available[priority].Sub(request)
	}
	for i, appWrapper := range queue {
		request := aggregateRequests(appWrapper)
		if request.Fits(available[int(appWrapper.Spec.Priority)]) {
			return appWrapper.DeepCopy(), i == len(queue)-1, nil // deep copy appWrapper
		}
	}
	return nil, false, nil
}

// Aggregated request by AppWrapper
func aggregateRequests(appWrapper *mcadv1beta1.AppWrapper) Weights {
	request := Weights{}
	for _, r := range appWrapper.Spec.Resources {
		request.AddProd(r.Replicas, NewWeights(r.Requests))
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
		return false
	}
}

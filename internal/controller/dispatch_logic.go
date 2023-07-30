/*
Copyright 2023.

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

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// Refresh and cache count of available gpus
// Add schedulable gpus, subtract gpus requested by scheduled non-AppWrapper non-terminated pods
func (r *AppWrapperReconciler) availableGpus(ctx context.Context) (int, error) {
	if !time.Now().After(r.WhenAvailable.Add(time.Minute)) {
		return r.AvailableGpus, nil
	}
	gpus := 0 // available gpus
	// add available gpus for each schedulable node
	nodes := &v1.NodeList{}
	if err := r.List(ctx, nodes, client.UnsafeDisableDeepCopy); err != nil {
		return 0, err
	}
	for _, node := range nodes.Items {
		// skip unschedulable nodes
		if node.Spec.Unschedulable {
			continue
		}
		// add allocatable gpus
		g := node.Status.Allocatable[nvidiaGpu]
		gpus += int(g.Value())
		// subtract gpus used by non-AppWrapper, non-terminated pods on this node
		fieldSelector, err := fields.ParseSelector(specNodeName + "=" + node.Name)
		if err != nil {
			return 0, err
		}
		pods := &v1.PodList{}
		if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
			client.MatchingFieldsSelector{Selector: fieldSelector}); err != nil {
			return 0, err
		}
		for _, pod := range pods.Items {
			if _, ok := pod.GetLabels()[uidLabel]; !ok && pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
				for _, container := range pod.Spec.Containers {
					g := container.Resources.Requests[nvidiaGpu]
					gpus -= int(g.Value())
				}
			}
		}
	}
	r.AvailableGpus = gpus
	r.WhenAvailable = time.Now()
	return gpus, nil
}

// Compute gpus reserved by AppWrappers at every priority level and sort queued AppWrappers
// AppWrappers in output queue must be cloned if mutated
func (r *AppWrapperReconciler) listAppWrappers(ctx context.Context) (map[int]int, []*mcadv1alpha1.AppWrapper, error) {
	aws := &mcadv1alpha1.AppWrapperList{}
	if err := r.List(ctx, aws, client.UnsafeDisableDeepCopy); err != nil {
		return nil, nil, err
	}
	gpus := map[int]int{}                 // gpus requested per priority level
	queue := []*mcadv1alpha1.AppWrapper{} // queued appwrappers
	for _, aw := range aws.Items {
		phase := aw.Status.Phase
		if p, ok := r.Phases[aw.UID]; ok {
			phase = p // use cached phase for better accuracy
		}
		if isActivePhase(phase) {
			gpus[int(aw.Spec.Priority)] += gpuRequest(&aw) // gpus requested by AppWrapper
		} else if phase == mcadv1alpha1.Queued {
			queue = append(queue, &aw)
		}
	}
	// propagate gpu reservations at all priority levels to all levels below
	Accumulate(gpus)
	// order AppWrapper queue based on priority and creation time
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Spec.Priority > queue[j].Spec.Priority {
			return true
		}
		if queue[i].Spec.Priority < queue[j].Spec.Priority {
			return false
		}
		return queue[j].CreationTimestamp.After(queue[i].CreationTimestamp.Time)
	})
	return gpus, queue, nil
}

// Find next AppWrapper to dispatch in queue order, return false AppWrapper is last in queue
func (r *AppWrapperReconciler) dispatchNext(ctx context.Context) (*mcadv1alpha1.AppWrapper, bool, error) {
	gpus, err := r.availableGpus(ctx)
	if err != nil {
		return nil, false, err
	}
	reservations, queue, err := r.listAppWrappers(ctx)
	if err != nil {
		return nil, false, err
	}
	for i, appWrapper := range queue {
		if gpuRequest(appWrapper) <= gpus-reservations[int(appWrapper.Spec.Priority)] {
			return appWrapper.DeepCopy(), i < len(queue)-1, nil
		}
	}
	return nil, false, nil
}

// Count gpu requested by AppWrapper
func gpuRequest(appWrapper *mcadv1alpha1.AppWrapper) int {
	gpus := 0
	for _, resource := range appWrapper.Spec.Resources {
		g := resource.Requests[nvidiaGpu]
		gpus += int(resource.Replicas) * int(g.Value())
	}
	return gpus
}

// Propagate gpu reservations at all priority levels to all levels below
func Accumulate(m map[int]int) {
	keys := make([]int, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i += 1
	}
	sort.Ints(keys)
	for i := len(keys) - 1; i > 0; i-- {
		m[keys[i-1]] += m[keys[i]]
	}
}

// Are resources reserved in this phase
func isActivePhase(phase mcadv1alpha1.AppWrapperPhase) bool {
	switch phase {
	case mcadv1alpha1.Dispatching, mcadv1alpha1.Running, mcadv1alpha1.Failed, mcadv1alpha1.Requeuing:
		return true
	default:
		return false
	}
}

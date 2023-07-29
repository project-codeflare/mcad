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
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// Test if AppWrapper fits available resources
func (r *AppWrapperReconciler) shouldDispatch(ctx context.Context, appwrapper *mcadv1alpha1.AppWrapper) (bool, error) {
	gpus := 0 // available gpus
	// add available gpus for each schedulable node
	nodes := &v1.NodeList{}
	if err := r.List(ctx, nodes, client.UnsafeDisableDeepCopy); err != nil {
		return false, err
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
			return false, err
		}
		pods := &v1.PodList{}
		if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
			client.MatchingFieldsSelector{Selector: fieldSelector}); err != nil {
			return false, err
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
	// subtract gpus reserved or used by active non-preemptable AppWrappers
	aws := &mcadv1alpha1.AppWrapperList{}
	if err := r.List(ctx, aws, client.UnsafeDisableDeepCopy); err != nil {
		return false, err
	}
	for _, aw := range aws.Items {
		if aw.UID != appwrapper.UID {
			phase := aw.Status.Phase
			if p, ok := r.Phases[aw.UID]; ok {
				phase = p // use cached phase for better accuracy
			}
			if isActivePhase(phase) && aw.Spec.Priority >= appwrapper.Spec.Priority {
				pods := &v1.PodList{}
				if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
					client.MatchingLabels{uidLabel: string(aw.UID)}); err != nil {
					return false, err
				}
				awGpus := gpuRequest(&aw)
				podGpus := 0 // gpus in use by non-terminated AppWrapper pods
				for _, pod := range pods.Items {
					if pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
						for _, container := range pod.Spec.Containers {
							g := container.Resources.Requests[nvidiaGpu]
							podGpus += int(g.Value())
						}
					}
				}
				// subtract max gpu usage among the two:
				// reserve the requested AppWrapper resources even if the pods are not all running
				// account for incorrect AppWrapper specs where actual use is greater than reservation
				if awGpus > podGpus {
					gpus -= awGpus
				} else {
					gpus -= podGpus
				}
			}
		}
	}
	fmt.Println(gpus)
	return gpuRequest(appwrapper) <= gpus, nil
}

// Count gpu requested by AppWrapper
func gpuRequest(appwrapper *mcadv1alpha1.AppWrapper) int {
	gpus := 0
	for _, resource := range appwrapper.Spec.Resources {
		g := resource.Requests[nvidiaGpu]
		gpus += int(resource.Replicas) * int(g.Value())
	}
	return gpus
}

// Is dispatch too slow?
func isSlowDispatch(appwrapper *mcadv1alpha1.AppWrapper) bool {
	return metav1.Now().After(appwrapper.Status.LastDispatchTime.Add(2 * time.Minute))
}

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
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// Parse raw resource into client object
func parseResource(appWrapper *mcadv1beta1.AppWrapper, raw []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, obj); err != nil {
		return nil, err
	}
	if obj.GetNamespace() == "<OWNER>" {
		obj.SetNamespace(appWrapper.Namespace)
	}
	return obj, nil
}

// Parse raw resources
func parseResources(appWrapper *mcadv1beta1.AppWrapper) ([]client.Object, error) {
	objects := make([]client.Object, len(appWrapper.Spec.Resources.GenericItems))
	for i, resource := range appWrapper.Spec.Resources.GenericItems {
		obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
		if err != nil {
			return nil, err
		}
		objects[i] = obj
	}
	return objects, nil
}

// Create wrapped resources, give up on first error
func (r *Runner) createResources(ctx context.Context, objects []client.Object) error {
	for _, obj := range objects {
		if err := r.Create(ctx, obj); err != nil {
			if !errors.IsAlreadyExists(err) { // ignore existing resources
				return err
			}
		}
	}
	return nil
}

// Check completion by looking at pods and wrapped resources
func (r *Runner) checkCompletion(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, counts *PodCounts) (bool, error) {
	if counts.Running > 0 ||
		counts.Other > 0 ||
		appWrapper.Spec.Scheduling.MinAvailable > 0 && counts.Succeeded < int(appWrapper.Spec.Scheduling.MinAvailable) {
		return false, nil
	}
	custom := false // at least one resource with completionstatus spec?
	for _, resource := range appWrapper.Spec.Resources.GenericItems {
		// skip resources without a completionstatus spec
		if resource.CompletionStatus != "" {
			custom = true
			obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
			if err != nil {
				return false, err
			}
			if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				return false, err
			}
			unstruct := obj.UnstructuredContent()
			done := false // at least one condition matching completionstatus spec?
			// check for a condition with status True and a type than contains one of the specified completion status key
			if status, ok := unstruct["status"].(map[string]interface{}); ok {
				if conditions, ok := status["conditions"].([]interface{}); ok {
					keys := strings.Split(resource.CompletionStatus, ",")
					for _, condition := range conditions {
						if c, ok := condition.(map[string]interface{}); ok {
							if t, ok := c["type"].(string); ok && c["status"] == "True" {
								for _, k := range keys {
									if strings.Contains(t, k) {
										done = true
										break
									}
								}
							}
						}
					}
				}
			}
			if !done {
				return false, nil
			}
		}
	}
	return custom || appWrapper.Spec.Scheduling.MinAvailable > 0 && counts.Succeeded >= int(appWrapper.Spec.Scheduling.MinAvailable), nil
}

// Delete wrapped resources, ignore errors, return count of pending deletions
func (r *Runner) deleteResources(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) int {
	log := log.FromContext(ctx)
	count := 0
	for _, resource := range appWrapper.Spec.Resources.GenericItems {
		obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
		if err != nil {
			log.Error(err, "Resource parsing error during deletion")
			continue // ignore parsing errors, there is no way we created this resource anyway
		}
		if err := r.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			if errors.IsNotFound(err) {
				continue // ignore missing resources
			}
			log.Error(err, "Resource deletion error")
		}
		count += 1 // no error deleting resource, resource therefore still exists
	}
	return count
}

// Forcefully delete wrapped resources and pods
func (r *Runner) forceDelete(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) {
	log := log.FromContext(ctx)
	// forcefully delete matching pods
	pod := &v1.Pod{}
	if err := r.DeleteAllOf(ctx, pod, client.GracePeriodSeconds(0),
		client.MatchingLabels{namespaceLabel: appWrapper.Namespace, nameLabel: appWrapper.Name}); err != nil {
		log.Error(err, "Error during forceful pod deletion")
	}
	for _, resource := range appWrapper.Spec.Resources.GenericItems {
		obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
		if err != nil {
			log.Error(err, "Resource parsing error during forceful deletion")
			continue // ignore parsing errors, there is no way we created this resource anyway
		}
		if err := r.Delete(ctx, obj, client.GracePeriodSeconds(0)); err != nil {
			if errors.IsNotFound(err) {
				continue // ignore missing resources
			}
			log.Error(err, "Forceful resource deletion error")
		}
	}
}

// Count AppWrapper pods
func (r *Runner) countPods(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) (*PodCounts, error) {
	// list matching pods
	pods := &v1.PodList{}
	if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
		client.MatchingLabels{namespaceLabel: appWrapper.Namespace, nameLabel: appWrapper.Name}); err != nil {
		return nil, err
	}
	counts := &PodCounts{}
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodSucceeded:
			counts.Succeeded += 1
		case v1.PodRunning:
			counts.Running += 1
		case v1.PodFailed:
			counts.Failed += 1
		default:
			counts.Other += 1
		}
	}
	return counts, nil
}

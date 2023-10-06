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
	"errors"
	"strings"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

const appWrapperNamespacePlaceholder = "<APPWRAPPER_NAMESPACE>"
const appWrapperNamePlaceholder = "<APPWRAPPER_NAME>"

// replace placeholders in map with AppWrapper metadata
func fixMap(appWrapper *mcadv1beta1.AppWrapper, m map[string]interface{}) {
	for k, v := range m {
		switch v := v.(type) {
		case string:
			if strings.HasPrefix(v, appWrapperNamespacePlaceholder) {
				m[k] = strings.Replace(v, appWrapperNamespacePlaceholder, appWrapper.Namespace, 1)
			} else if strings.HasPrefix(v, appWrapperNamePlaceholder) {
				m[k] = strings.Replace(v, appWrapperNamePlaceholder, appWrapper.Name, 1)
			}
		case map[string]interface{}:
			fixMap(appWrapper, v)
		case []interface{}:
			fixArray(appWrapper, v)
		}
	}
}

// replace placeholders in array with AppWrapper metadata
func fixArray(appWrapper *mcadv1beta1.AppWrapper, a []interface{}) {
	for k, v := range a {
		switch v := v.(type) {
		case string:
			if strings.HasPrefix(v, appWrapperNamespacePlaceholder) {
				a[k] = strings.Replace(v, appWrapperNamespacePlaceholder, appWrapper.Namespace, 1)
			} else if strings.HasPrefix(v, appWrapperNamePlaceholder) {
				a[k] = strings.Replace(v, appWrapperNamePlaceholder, appWrapper.Name, 1)
			}
		case map[string]interface{}:
			fixMap(appWrapper, v)
		case []interface{}:
			fixArray(appWrapper, v)
		}
	}
}

// Parse raw resource into client object
func parseResource(appWrapper *mcadv1beta1.AppWrapper, raw []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, obj); err != nil {
		return nil, err
	}
	fixMap(appWrapper, obj.UnstructuredContent())
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
			if !apierrors.IsAlreadyExists(err) { // ignore existing resources
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
			var derr *discovery.ErrGroupDiscoveryFailed
			var merr *meta.NoKindMatchError
			if apierrors.IsNotFound(err) || errors.As(err, &derr) || errors.As(err, &merr) {
				continue // ignore missing resources and api resources
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
			var derr *discovery.ErrGroupDiscoveryFailed
			var merr *meta.NoKindMatchError
			if apierrors.IsNotFound(err) || errors.As(err, &derr) || errors.As(err, &merr) {
				continue // ignore missing resources and api resources
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

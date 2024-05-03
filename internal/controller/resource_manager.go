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
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// PodCounts summarize the status of the pods associated with one AppWrapper
type PodCounts struct {
	Other     int
	Running   int
	Succeeded int
}

// Fix labels in maps
func fixMap(appWrapper *mcadv1beta1.AppWrapper, m map[string]interface{}) {
	// inject placeholder in pod specs
	if spec, ok := m["spec"].(map[string]interface{}); ok {
		if _, ok := spec["containers"]; ok {
			metadata, ok := m["metadata"].(map[string]interface{})
			if !ok {
				metadata = map[string]interface{}{}
				m["metadata"] = metadata
			}
			labels, ok := metadata["labels"].(map[string]interface{})
			if !ok {
				labels = map[string]interface{}{}
				metadata["labels"] = labels
			}
			labels[nameLabel] = "placeholder"
		}
	}
	// replace placeholder with actual labels
	if labels, ok := m["labels"].(map[string]interface{}); ok {
		if _, ok := labels[nameLabel]; ok {
			labels[namespaceLabel] = appWrapper.Namespace
			labels[nameLabel] = appWrapper.Name
		}
	}
	// visit submaps and arrays
	for _, v := range m {
		switch v := v.(type) {
		case map[string]interface{}:
			fixMap(appWrapper, v)
		case []interface{}:
			fixArray(appWrapper, v)
		}
	}
}

// Fix labels in arrays
func fixArray(appWrapper *mcadv1beta1.AppWrapper, a []interface{}) {
	// visit submaps and arrays
	for _, v := range a {
		switch v := v.(type) {
		case map[string]interface{}:
			fixMap(appWrapper, v)
		case []interface{}:
			fixArray(appWrapper, v)
		}
	}
}

// Parse raw resource into unstructured object
func parseResource(appWrapper *mcadv1beta1.AppWrapper, raw []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, obj); err != nil {
		return nil, err
	}
	fixMap(appWrapper, obj.UnstructuredContent())
	namespace := obj.GetNamespace()
	if namespace == "" {
		obj.SetNamespace(appWrapper.Namespace)
	} else if namespace != appWrapper.Namespace {
		return nil, fmt.Errorf("generic item namespace \"%s\" is different from AppWrapper namespace \"%s\"", namespace, appWrapper.Namespace)
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

// Create wrapped resources, give up on first error, decide if error is fatal
func (r *AppWrapperReconciler) createResources(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) (error, bool) {
	objects, err := parseResources(appWrapper)
	if err != nil {
		return err, true // fatal
	}
	for _, obj := range objects {
		if err := r.Create(ctx, obj); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue // ignore existing resources
			}
			return err, meta.IsNoMatchError(err) || apierrors.IsInvalid(err) // fatal
		}
	}
	return nil, false
}

// Assess successful completion of AppWrapper by looking at pods and wrapped resources
func (r *AppWrapperReconciler) isSuccessful(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, counts *PodCounts) (bool, error) {
	// To succeed we need at least one successful pods and no running, failed, and other pods
	if counts.Running > 0 || counts.Other > 0 || counts.Succeeded < 1 {
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
			success := false // at least one condition matching completionstatus spec?
			// check for a condition with status True and a type than contains one of the specified completion status key
			if status, ok := unstruct["status"].(map[string]interface{}); ok {
				if conditions, ok := status["conditions"].([]interface{}); ok {
					keys := strings.Split(resource.CompletionStatus, ",")
					for _, condition := range conditions {
						if c, ok := condition.(map[string]interface{}); ok {
							if t, ok := c["type"].(string); ok && c["status"] == "True" {
								for _, k := range keys {
									if strings.Contains(strings.ToLower(t), strings.ToLower(k)) {
										success = true
										break
									}
								}
							}
						}
					}
				}
			}
			if !success {
				return false, nil
			}
		}
	}
	// To succeed we need to pass the custom completion status check or have enough successful pods if MinAvailable >= 0
	targetSucceeded := 1
	if appWrapper.Spec.Scheduling.MinAvailable > int32(targetSucceeded) {
		targetSucceeded = int(appWrapper.Spec.Scheduling.MinAvailable)
	}
	return custom || (appWrapper.Spec.Scheduling.MinAvailable >= 0 && counts.Succeeded >= targetSucceeded), nil
}

// Delete wrapped resources, forcing deletion of pods and wrapped resources if enabled
func (r *AppWrapperReconciler) deleteResources(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, timestamp metav1.Time) bool {
	log := log.FromContext(ctx)
	remaining := 0
	for _, resource := range appWrapper.Spec.Resources.GenericItems {
		obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
		if err != nil {
			log.Error(err, "Parsing error")
			continue
		}
		if err := r.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "Deletion error")
			}
			continue
		}
		remaining++ // no error deleting resource, resource therefore still exists
	}
	if appWrapper.Spec.Scheduling.Requeuing.ForceDeletionTimeInSeconds <= 0 {
		// force deletion is not enabled, return true iff no resources were found
		return remaining == 0
	}
	pods := &v1.PodList{Items: []v1.Pod{}}
	if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
		client.MatchingLabels{namespaceLabel: appWrapper.Namespace, nameLabel: appWrapper.Name}); err != nil {
		log.Error(err, "Pod list error")
	}
	if remaining == 0 && len(pods.Items) == 0 {
		// no resources, no pods, deletion is complete
		return true
	}
	if !metav1.Now().After(timestamp.Add(time.Duration(appWrapper.Spec.Scheduling.Requeuing.ForceDeletionTimeInSeconds) * time.Second)) {
		// wait before forcing deletion and simply requeue deletion
		return false
	}
	if len(pods.Items) > 0 {
		// force deletion of pods first
		for _, pod := range pods.Items {
			if err := r.Delete(ctx, &pod, client.GracePeriodSeconds(0)); err != nil {
				log.Error(err, "Forceful pod deletion error")
			}
		}
	} else {
		// force deletion of wrapped resources once pods are gone
		for _, resource := range appWrapper.Spec.Resources.GenericItems {
			obj, err := parseResource(appWrapper, resource.GenericTemplate.Raw)
			if err != nil {
				log.Error(err, "Parsing error")
				continue
			}
			if err := r.Delete(ctx, obj, client.GracePeriodSeconds(0)); err != nil && !apierrors.IsNotFound(err) {
				log.Error(err, "Forceful deletion error")
			}
		}
	}
	// requeue deletion
	return false
}

// Count AppWrapper pods
func (r *AppWrapperReconciler) countPods(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) (*PodCounts, error) {
	// list matching pods
	pods := &v1.PodList{}
	if err := r.List(ctx, pods,
		client.MatchingLabels{nameLabel: appWrapper.Name}); err != nil {
		return nil, err
	}
	counts := &PodCounts{}
	for _, pod := range pods.Items {
		namespace := pod.Labels[namespaceLabel]
		switch pod.Status.Phase {
		case v1.PodSucceeded:
			if namespace == appWrapper.Namespace || namespace == "" {
				counts.Succeeded += 1 // for backward compatibility count pods missing namespace label
			}
		case v1.PodRunning:
			if pod.DeletionTimestamp.IsZero() {
				if namespace == appWrapper.Namespace || namespace == "" {
					counts.Running += 1 // for backward compatibility count pods missing namespace label
				}
			} else {
				if namespace == appWrapper.Namespace {
					counts.Other += 1
				}
			}
		default:
			if namespace == appWrapper.Namespace {
				counts.Other += 1
			}
		}
	}
	return counts, nil
}

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
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// Parse raw resource into client object
func (r *AppWrapperReconciler) parseResource(appwrapper *mcadv1alpha1.AppWrapper, raw []byte) (client.Object, error) {
	into, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}
	obj := into.(client.Object)
	namespaced, err := r.IsObjectNamespaced(obj)
	if err != nil {
		return nil, err
	}
	if namespaced && obj.GetNamespace() == "" {
		obj.SetNamespace(appwrapper.ObjectMeta.Namespace) // use AppWrapper namespace as default
	}
	obj.SetLabels(map[string]string{label: appwrapper.ObjectMeta.Name}) // add AppWrapper label
	return obj, nil
}

// Parse raw resources
func (r *AppWrapperReconciler) parseResources(appwrapper *mcadv1alpha1.AppWrapper) ([]client.Object, error) {
	objects := make([]client.Object, len(appwrapper.Spec.Resources))
	var err error
	for i, resource := range appwrapper.Spec.Resources {
		objects[i], err = r.parseResource(appwrapper, resource.Template.Raw)
		if err != nil {
			return nil, err
		}
	}
	return objects, err
}

// Create wrapped resources
func (r *AppWrapperReconciler) createResources(ctx context.Context, objects []client.Object) error {
	for _, obj := range objects {
		if err := r.Create(ctx, obj); err != nil {
			if !errors.IsAlreadyExists(err) { // ignore existing resources
				return err
			}
		}
	}
	return nil
}

// Delete wrapped resources, returning count of pending deletions
func (r *AppWrapperReconciler) deleteResources(ctx context.Context, appwrapper *mcadv1alpha1.AppWrapper) int {
	log := log.FromContext(ctx)
	count := 0
	for _, resource := range appwrapper.Spec.Resources {
		obj, err := r.parseResource(appwrapper, resource.Template.Raw)
		if err != nil {
			log.Error(err, "Resource parsing error during deletion")
			continue
		}
		background := metav1.DeletePropagationBackground
		if err := r.Delete(ctx, obj, &client.DeleteOptions{PropagationPolicy: &background}); err != nil {
			if errors.IsNotFound(err) {
				continue // ignore missing resources
			}
			log.Error(err, "Resource deletion error")
		}
		count += 1
	}
	return count
}

// Monitor AppWrapper pods
func (r *AppWrapperReconciler) monitorPods(ctx context.Context, appwrapper *mcadv1alpha1.AppWrapper) (*PodCounts, error) {
	// list matching pods
	pods := &v1.PodList{}
	if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy, client.MatchingLabels{label: appwrapper.ObjectMeta.Name}); err != nil {
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

// Is deletion too slow?
func isSlowDeletion(appwrapper *mcadv1alpha1.AppWrapper) bool {
	return metav1.Now().After(appwrapper.ObjectMeta.DeletionTimestamp.Add(2 * time.Minute))
}

// Is requeuing too slow?
func isSlowRequeuing(appwrapper *mcadv1alpha1.AppWrapper) bool {
	return metav1.Now().After(appwrapper.Status.LastRequeuingTime.Add(2 * time.Minute))
}

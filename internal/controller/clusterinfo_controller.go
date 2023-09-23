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
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// ClusterInfoReconciler reconciles a ClusterInfo object
type ClusterInfoReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Namespace string
	Name      string
}

// Reconcile ClusterInfo object
func (r *ClusterInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// only reconcile cluster info for this cluster
	if req.Namespace != r.Namespace || req.Name != r.Name {
		return ctrl.Result{}, nil
	}
	// get cluster info if it already exists
	clusterInfo := &mcadv1beta1.ClusterInfo{ObjectMeta: metav1.ObjectMeta{Namespace: r.Namespace, Name: r.Name}}
	update := false
	if err := r.Client.Get(ctx, req.NamespacedName, clusterInfo); err == nil {
		// do not update if too early
		now := time.Now()
		expiration := clusterInfo.Status.Time.Add(clusterInfoTimeout)
		if expiration.After(now) {
			// requeue to update after timeout
			return ctrl.Result{RequeueAfter: expiration.Sub(now)}, nil
		}
		update = true // cluster info already exists
	}
	// compute available capacity
	capacity, err := r.computeCapacity(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	clusterInfo.Status.Capacity = capacity
	clusterInfo.Status.Time = metav1.Now()
	if update {
		// update status of existing cluster info object
		if err := r.Status().Update(ctx, clusterInfo); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// create new cluster info object
		if err := r.Create(ctx, clusterInfo); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: clusterInfoTimeout}, nil
}

// Compute available cluster capacity
func (r *ClusterInfoReconciler) computeCapacity(ctx context.Context) (v1.ResourceList, error) {
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
	return capacity.AsResources(), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.ClusterInfo{}).
		Complete(r)
}

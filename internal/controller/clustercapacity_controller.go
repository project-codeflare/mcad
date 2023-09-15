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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// ClusterCapacityReconciler reconciles a ClusterCapacity object
type ClusterCapacityReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NextSync time.Time // when to refresh cluster capacity
	Mode     string    // default, dispatcher, runner
}

//+kubebuilder:rbac:groups=mcad.codeflare.dev,resources=clustercapacities,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcad.codeflare.dev,resources=clustercapacities/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcad.codeflare.dev,resources=clustercapacities/finalizers,verbs=update

func (r *ClusterCapacityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO check ClusterCapacity id matches cluster id

	if !time.Now().After(r.NextSync) {
		return ctrl.Result{Requeue: true}, nil
	}
	cc := &mcadv1beta1.ClusterCapacity{}
	if err := r.Get(ctx, req.NamespacedName, cc); err != nil {
		panic(err)
	}
	capacity := Weights{}
	// add allocatable capacity for each schedulable node
	nodes := &v1.NodeList{}
	if err := r.List(ctx, nodes, client.UnsafeDisableDeepCopy); err != nil {
		return ctrl.Result{}, err
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
			return ctrl.Result{}, err
		}
		pods := &v1.PodList{}
		if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
			client.MatchingFieldsSelector{Selector: fieldSelector}); err != nil {
			return ctrl.Result{}, err
		}
		for _, pod := range pods.Items {
			if _, ok := pod.GetLabels()[nameLabel]; !ok && pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
				for _, container := range pod.Spec.Containers {
					capacity.Sub(NewWeights(container.Resources.Requests))
				}
			}
		}
	}
	cc.Status.Capacity = capacity.Resources()
	if err := r.Status().Update(ctx, cc); err != nil {
		return ctrl.Result{}, err
	}
	r.NextSync = time.Now().Add(clusterCapacityTimeout)
	return ctrl.Result{RequeueAfter: clusterCapacityTimeout}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterCapacityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Mode == "dispatcher" {
		return nil
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.ClusterCapacity{}).
		Complete(r)
}

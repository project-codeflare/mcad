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

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// ClusterInfoReconciler reconciles a ClusterInfo object
type ClusterInfoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// permission to edit clusterinfo

//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=clusterinfo,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=clusterinfo/status,verbs=get;update;patch

// Reconcile ClusterInfo object
func (r *ClusterInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	clusterInfo := &mcadv1beta1.ClusterInfo{}
	if err := r.Client.Get(ctx, req.NamespacedName, clusterInfo); err == nil {
		// do not recompute cluster capacity if old value has not expired yet
		now := time.Now()
		expiration := clusterInfo.Status.Time.Add(clusterInfoTimeout)
		if expiration.After(now) {
			// requeue to update after expiration
			return ctrl.Result{RequeueAfter: expiration.Sub(now)}, nil
		}
	} else {
		return ctrl.Result{}, err
	}
	// compute available capacity
	capacity, err := r.computeCapacity(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	clusterInfo.Status.Capacity = capacity.AsResources()
	clusterInfo.Status.Time = metav1.Now()
	// update cluster info status
	if err := r.Status().Update(ctx, clusterInfo); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: clusterInfoTimeout}, nil
}

// Compute available cluster capacity
func (r *ClusterInfoReconciler) computeCapacity(ctx context.Context) (Weights, error) {
	capacity := Weights{}
	// add allocatable capacity for each schedulable node
	nodes := &v1.NodeList{}
	if err := r.List(ctx, nodes, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
LOOP:
	for _, node := range nodes.Items {
		// skip unschedulable nodes
		if node.Spec.Unschedulable {
			continue
		}
		for _, taint := range node.Spec.Taints {
			if taint.Effect == v1.TaintEffectNoSchedule || taint.Effect == v1.TaintEffectNoExecute {
				continue LOOP
			}
		}
		// compute allocatable capacity on the node
		nodeCapacity := NewWeights(node.Status.Allocatable)
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
				nodeCapacity.Sub(NewWeightsForPod(&pod))
			}
		}
		// add allocatable capacity on the node
		capacity.Add(nodeCapacity)
		updateCapacityMetrics(capacity, node)
	}
	return capacity, nil
}

// Update capacity metrics
func updateCapacityMetrics(capacity Weights, node v1.Node) {
	capacityCpu, err := Dec2float64(capacity["cpu"])
	if err != nil {
		mcadLog.Error(err, "Unable to get CPU capacity", "node", node.Name)
	} else {
		totalCapacityCpu.WithLabelValues(node.Name).Set(capacityCpu)
	}

	capacityMemory, err := Dec2float64(capacity["memory"])
	if err != nil {
		mcadLog.Error(err, "Unable to get memory capacity", "node", node.Name)
	} else {
		totalCapacityMemory.WithLabelValues(node.Name).Set(capacityMemory)
	}

	if val, exists := capacity["nvidia.com/gpu"]; exists {
		capacityGpu, err := Dec2float64(val)
		if err != nil {
			mcadLog.Error(err, "Unable to get GPU capacity", "node", node.Name)
		} else {
			totalCapacityGpu.WithLabelValues(node.Name).Set(capacityGpu)
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// index pods with nodeName key
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, specNodeName, func(obj client.Object) []string {
		pod := obj.(*v1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.ClusterInfo{}).
		Complete(r)
}

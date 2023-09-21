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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// AppWrapperReconciler reconciles an AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Events chan event.GenericEvent
	Scheme *runtime.Scheme
	Cache  map[types.UID]*CachedAppWrapper // cache appWrapper updates for write/read consistency
	Mode   string                          // default, dispatcher, runner
}

const (
	nameLabel      = "mcad.codeflare.dev"           // owner name label for wrapped resources
	namespaceLabel = "mcad.codeflare.dev/namespace" // owner namespace label for wrapped resources
	finalizer      = "mcad.codeflare.dev/finalizer" // finalizer name
	nvidiaGpu      = "nvidia.com/gpu"               // GPU resource name
	specNodeName   = ".spec.nodeName"               // key to index pods based on node placement
)

// PodCounts summarize the status of the pods associated with one AppWrapper
type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile one AppWrapper or dispatch next AppWrapper
// Normal reconciliations "namespace/name" implement all phase transitions except for Queued->Dispatching
// Queued->Dispatching transitions happen as part of a special "*/*" reconciliation
// In a "*/*" reconciliation, we first decide the AppWrapper to dispatch (dispatchNext)
// We then dispatch this AppWrapper (Queued->Dispatching transition)
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// log.Info("Reconcile")

	// handle dispatching first

	// req == "*/*", find the next AppWrapper to dispatch and dispatch this AppWrapper
	if req.Name == "*" {
		if r.Mode == "runner" {
			return ctrl.Result{}, nil
		}
		appWrapper, last, err := r.dispatchNext(ctx) // last == is last appWrapper in queue?
		if err != nil {
			return ctrl.Result{}, err
		}
		if appWrapper == nil { // no appWrapper eligible for dispatch
			return ctrl.Result{RequeueAfter: dispatchDelay}, nil // retry to dispatch later
		}
		// abort and requeue reconciliation if reconciler cache is stale
		if err := r.checkCachedPhase(appWrapper); err != nil {
			return ctrl.Result{}, err
		}
		if appWrapper.Status.Phase != mcadv1beta1.Queued {
			// this check should be redundant but better be defensive
			return ctrl.Result{}, errors.New("not queued")
		}
		// set dispatching timestamp and status
		appWrapper.Status.LastDispatchTime = metav1.Now()
		if _, err := r.updateStatus(ctx, appWrapper, mcadv1beta1.Dispatching); err != nil {
			return ctrl.Result{}, err
		}
		if last {
			return ctrl.Result{RequeueAfter: dispatchDelay}, nil // retry to dispatch later
		}
		return ctrl.Result{Requeue: true}, nil // requeue to continue to dispatch queued appWrappers
	}

	// normal reconciliation starts here

	appWrapper := &mcadv1beta1.AppWrapper{}

	// get deep copy of AppWrapper object in reconciler cache
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	// abort and requeue reconciliation if reconciler cache is stale
	if err := r.checkCachedPhase(appWrapper); err != nil {
		return ctrl.Result{}, err
	}

	// sync status
	if len(appWrapper.Spec.DispatcherStatus.Conditions) > len(appWrapper.Status.Conditions) {
		appWrapper.Status = appWrapper.Spec.DispatcherStatus
		if err := r.Status().Update(ctx, appWrapper); err != nil {
			return ctrl.Result{}, err // etcd update failed, abort and requeue reconciliation
		}
		r.addCachedPhase(appWrapper)
		return ctrl.Result{}, nil
	}

	// first handle deletion
	if !appWrapper.DeletionTimestamp.IsZero() && appWrapper.Status.Phase != mcadv1beta1.Deleted {
		if r.Mode == "dispatcher" {
			return ctrl.Result{}, nil
		}
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			return ctrl.Result{Requeue: true}, nil // requeue reconciliation
		}
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Deleted)
	}

	// handle all other phases including the default empty phase

	switch appWrapper.Status.Phase {
	case mcadv1beta1.Deleted:
		if r.Mode == "runner" {
			return ctrl.Result{}, nil
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		r.deleteCachedPhase(appWrapper) // remove appWrapper from cache
		r.triggerDispatchNext()         // cluster may have more available capacity
		return ctrl.Result{}, nil

	case mcadv1beta1.Failed:
		// nothing to reconcile
		return ctrl.Result{}, nil

	case mcadv1beta1.Succeeded, mcadv1beta1.Queued:
		if r.Mode == "runner" {
			return ctrl.Result{}, nil
		}
		r.triggerDispatchNext()
		return ctrl.Result{}, nil

	case mcadv1beta1.Requeuing:
		if r.Mode == "dispatcher" {
			return ctrl.Result{}, nil
		}
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			if isSlowRequeuing(appWrapper) {
				// give up requeuing and fail instead
				return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed)
			} else {
				return ctrl.Result{Requeue: true}, nil // requeue reconciliation
			}
		}
		// update status to queued
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued)

	case mcadv1beta1.Dispatching:
		if r.Mode == "dispatcher" {
			return ctrl.Result{}, nil
		}
		// dispatching is taking too long?
		if isSlowDispatching(appWrapper) {
			// set requeuing or failed status
			return r.requeueOrFail(ctx, appWrapper)
		}
		// create wrapped resources
		objects, err := r.parseResources(appWrapper)
		if err != nil {
			log.Error(err, "Resource parsing error during creation")
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed)
		}
		// create wrapped resources
		if err := r.createResources(ctx, objects); err != nil {
			return ctrl.Result{}, err
		}
		// set running status only after successfully requesting the creation of all resources
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running)

	case mcadv1beta1.Running:
		if r.Mode == "dispatcher" {
			return ctrl.Result{}, nil
		}
		// check AppWrapper health
		counts, err := r.monitorPods(ctx, appWrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		if counts.Failed > 0 || isSlowRunning(appWrapper) && (counts.Other > 0 || counts.Running < int(appWrapper.Spec.MinPods)) {
			// set requeuing or failed status
			return r.requeueOrFail(ctx, appWrapper)
		}
		if appWrapper.Spec.MinPods > 0 && counts.Succeeded >= int(appWrapper.Spec.MinPods) && counts.Running == 0 && counts.Other == 0 {
			// set succeeded status
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Succeeded)
		}
		return ctrl.Result{RequeueAfter: runDelay}, nil // check again soon

	default: // empty phase
		if r.Mode == "runner" {
			return ctrl.Result{}, nil
		}
		// add finalizer
		if controllerutil.AddFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status only after adding finalizer
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppWrapperReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// index pods with nodeName key
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, specNodeName, func(obj client.Object) []string {
		pod := obj.(*v1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}
	builder := ctrl.NewControllerManagedBy(mgr).For(&mcadv1beta1.AppWrapper{})
	if r.Mode != "dispatcher" {
		// watch AppWrapper pods in addition to AppWrappers so we can react to pod failures and other pod events
		builder = builder.Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc))
	}
	if r.Mode != "runner" {
		// watch for cluster capacity changes and trigger dispatch event
		// watch for dispatch event (on queued, deleted, succeeded states and cluster capacity changes)
		builder = builder.
			Watches(&mcadv1beta1.ClusterInfo{}, handler.EnqueueRequestsFromMapFunc(r.clusterInfoMapFunc)).
			WatchesRawSource(&source.Channel{Source: r.Events}, &handler.EnqueueRequestForObject{})
	}
	return builder.Complete(r)
}

// Map labelled pods to corresponding AppWrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if name, ok := pod.Labels[nameLabel]; ok {
		if namespace, ok := pod.Labels[namespaceLabel]; ok {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
		}
	}
	return nil
}

// Trigger dispatchNext on cluster capacity change
func (r *AppWrapperReconciler) clusterInfoMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	r.triggerDispatchNext()
	return nil
}

// Update AppWrapper status
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, phase mcadv1beta1.AppWrapperPhase) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	now := metav1.Now()
	if phase == mcadv1beta1.Dispatching {
		now = appWrapper.Status.LastDispatchTime // ensure condition timestamp is consistent with status field
	} else if phase == mcadv1beta1.Requeuing {
		now = appWrapper.Status.LastRequeuingTime // ensure condition timestamp is consistent with status field
	}
	// log transition as condition
	condition := mcadv1beta1.AppWrapperCondition{LastTransitionTime: now, Reason: string(phase)}
	appWrapper.Status.Conditions = append(appWrapper.Status.Conditions, condition)
	appWrapper.Status.Phase = phase
	if r.Mode == "dispatcher" {
		// populate DispatcherStatus instead of Status when running in dispatcher mode
		appWrapper.Spec.DispatcherStatus = appWrapper.Status
		if err := r.Update(ctx, appWrapper); err != nil {
			return ctrl.Result{}, err // etcd update failed, abort and requeue reconciliation
		}
	} else if err := r.Status().Update(ctx, appWrapper); err != nil {
		return ctrl.Result{}, err // etcd update failed, abort and requeue reconciliation
	}
	log.Info(string(phase))
	// cache AppWrapper status
	r.addCachedPhase(appWrapper)
	return ctrl.Result{}, nil
}

// Set requeuing or failed status depending on retry count
func (r *AppWrapperReconciler) requeueOrFail(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) (ctrl.Result, error) {
	if appWrapper.Status.Requeued < appWrapper.Spec.MaxRetries {
		appWrapper.Status.Requeued += 1
		appWrapper.Status.LastRequeuingTime = metav1.Now()
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Requeuing)
	}
	return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed)
}

// Trigger dispatch
func (r *AppWrapperReconciler) triggerDispatchNext() {
	select {
	case r.Events <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "*", Name: "*"}}}:
	default:
	}
}

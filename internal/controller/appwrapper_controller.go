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
	"errors"
	"time"

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

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// AppWrapperReconciler reconciles an AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Events        chan event.GenericEvent
	Scheme        *runtime.Scheme
	Cache         map[types.UID]*mcadv1alpha1.AppWrapper // cache appWrapper updates to improve dispatch accuracy
	Capacity      Weights                                // cluster capacity available to mcad
	WhenAvailable time.Time                              // when last computed
}

const (
	namespaceLabel = "mcad.codeflare.dev/namespace" // owner namespace label for wrapped resources
	nameLabel      = "mcad.codeflare.dev/name"      // owner name label for wrapped resources
	uidLabel       = "mcad.codeflare.dev/uid"       // owner UID label for wrapped resources
	finalizer      = "mcad.codeflare.dev/finalizer" // AppWrapper finalizer name
	nvidiaGpu      = "nvidia.com/gpu"               // GPU resource name
	specNodeName   = ".spec.nodeName"               // pod node name field
)

// PodCounts summarizes the status of the pods associated with one AppWrapper
type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile one AppWrapper or dispatch next AppWrapper
// Queued AppWrapper are dispatched as part of a special "*/*" reconciliation cycle
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// log.Info("Reconcile")

	// req == "*/*", do not reconcile a specific appWrapper
	// instead dispatch appWrapper proposed by dispatchNext if any
	if req.Name == "*" {
		appWrapper, last, err := r.dispatchNext(ctx) // last == is last appWrapper in queue?
		if err != nil {
			return ctrl.Result{}, err
		}
		if appWrapper == nil { // no appWrapper eligible for dispatch
			return ctrl.Result{RequeueAfter: time.Minute}, nil // retry to dispatch later
		}
		// requeue reconciliation if reconciler cache is not updated
		if err := r.checkCache(appWrapper); err != nil {
			return ctrl.Result{}, err
		}
		if appWrapper.Status.Phase != mcadv1alpha1.Queued {
			return ctrl.Result{}, errors.New("not queued")
		}
		// set dispatching timestamp and status
		appWrapper.Status.LastDispatchTime = metav1.Now()
		if _, err := r.updateStatus(ctx, appWrapper, mcadv1alpha1.Dispatching); err != nil {
			return ctrl.Result{}, err
		}
		if last {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // retry to dispatch later
		}
		return ctrl.Result{Requeue: true}, nil // requeue to continue to dispatch queued appWrappers
	}

	appWrapper := &mcadv1alpha1.AppWrapper{}

	// get deep copy of AppWrapper object in reconciler cache
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	// requeue reconciliation if reconciler cache is not updated
	if err := r.checkCache(appWrapper); err != nil {
		return ctrl.Result{}, err
	}

	// deletion requested
	if !appWrapper.DeletionTimestamp.IsZero() {
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // requeue reconciliation
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		log.Info("Deleted")
		delete(r.Cache, appWrapper.UID) // remove appWrapper from cache
		if isActivePhase(appWrapper.Status.Phase) {
			r.triggerDispatchNext() // cluster may have more available capacity
		}
		return ctrl.Result{}, nil
	}

	switch appWrapper.Status.Phase {
	case mcadv1alpha1.Succeeded, mcadv1alpha1.Failed:
		// nothing to reconcile
		return ctrl.Result{}, nil

	case mcadv1alpha1.Queued:
		// this AppWrapper may not be the head of the queue, trigger dispatch in queue order
		r.triggerDispatchNext()
		return ctrl.Result{}, nil

	case mcadv1alpha1.Requeuing:
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			if isSlowRequeuing(appWrapper) {
				// give up requeuing and fail instead
				return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Failed)
			} else {
				return ctrl.Result{RequeueAfter: time.Minute}, nil // requeue reconciliation
			}
		}
		// update status to queued
		return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Queued)

	case mcadv1alpha1.Dispatching:
		// dispatching is taking too long?
		if isSlowDispatch(appWrapper) {
			// set requeuing or failed status
			return r.requeueOrFail(ctx, appWrapper)
		}
		// create wrapped resources
		objects, err := r.parseResources(appWrapper)
		if err != nil {
			log.Error(err, "Resource parsing error during creation")
			return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Failed)
		}
		// create wrapped resources
		if err := r.createResources(ctx, objects); err != nil {
			return ctrl.Result{}, err
		}
		// set running status only after successfully requesting the creation of all resources
		return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Running)

	case mcadv1alpha1.Running:
		// check AppWrapper health
		counts, err := r.monitorPods(ctx, appWrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		slow := isSlowDispatch(appWrapper)
		if counts.Failed > 0 || slow && (counts.Other > 0 || counts.Running < int(appWrapper.Spec.MinPods)) {
			// set requeuing or failed status
			return r.requeueOrFail(ctx, appWrapper)
		}
		if appWrapper.Spec.MinPods > 0 && counts.Succeeded >= int(appWrapper.Spec.MinPods) && counts.Running == 0 && counts.Other == 0 {
			// set succeeded status
			return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Succeeded)
		}
		if !slow {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // check again soon
		}
		return ctrl.Result{}, nil // only check again on pod change

	default: // empty phase
		// add finalizer
		if controllerutil.AddFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status only after adding finalizer
		return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Queued)
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
	// watch AppWrapper pods in addition to AppWrappers
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1alpha1.AppWrapper{}).
		WatchesRawSource(&source.Channel{Source: r.Events}, &handler.EnqueueRequestForObject{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map labelled pods to AppWrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if namespace, ok := pod.Labels[namespaceLabel]; ok {
		if name, ok := pod.Labels[nameLabel]; ok {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
		}
	}
	return nil
}

// Update AppWrapper status
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, appWrapper *mcadv1alpha1.AppWrapper, phase mcadv1alpha1.AppWrapperPhase) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	now := metav1.Now()
	activeBefore := isActivePhase(appWrapper.Status.Phase)
	if phase == mcadv1alpha1.Dispatching {
		now = appWrapper.Status.LastDispatchTime // ensure timestamps are consistent
	} else if phase == mcadv1alpha1.Requeuing {
		now = appWrapper.Status.LastRequeuingTime // ensure timestamps are consistent
	}
	// log transition as condition
	condition := mcadv1alpha1.AppWrapperCondition{LastTransitionTime: now, Reason: string(phase)}
	appWrapper.Status.Conditions = append(appWrapper.Status.Conditions, condition)
	appWrapper.Status.Phase = phase
	if err := r.Status().Update(ctx, appWrapper); err != nil {
		return ctrl.Result{}, err
	}
	log.Info(string(phase))
	r.Cache[appWrapper.UID] = appWrapper // cache updated appWrapper
	// this appWrapper is a deep copy of the reconciler cache already (obtained with r.Get)
	// this appWrapper should not be mutated beyond this point
	activeAfter := isActivePhase(phase)
	if activeBefore && !activeAfter {
		r.triggerDispatchNext() // cluster may have more available capacity
	}
	return ctrl.Result{}, nil
}

// Set requeuing or failed status depending on retry count
func (r *AppWrapperReconciler) requeueOrFail(ctx context.Context, appWrapper *mcadv1alpha1.AppWrapper) (ctrl.Result, error) {
	if appWrapper.Status.Requeued < appWrapper.Spec.MaxRetries {
		appWrapper.Status.Requeued += 1
		appWrapper.Status.LastRequeuingTime = metav1.Now()
		return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Requeuing)
	}
	return r.updateStatus(ctx, appWrapper, mcadv1alpha1.Failed)
}

// Trigger dispatch
func (r *AppWrapperReconciler) triggerDispatchNext() {
	select {
	case r.Events <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "*", Name: "*"}}}:
	default:
	}
}

// Check whether our cache and reconciler cache appear to be in sync
func (r *AppWrapperReconciler) checkCache(appWrapper *mcadv1alpha1.AppWrapper) error {
	// check number of conditions is the same
	if cached, ok := r.Cache[appWrapper.UID]; ok && len(cached.Status.Conditions) != len(appWrapper.Status.Conditions) {
		// reconciler cache and our cache are out of sync
		delete(r.Cache, appWrapper.UID)     // remove appWrapper from cache
		return errors.New("cache conflict") // force redo
	}
	return nil
}

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
	Phases        map[types.UID]mcadv1alpha1.AppWrapperPhase // cache phases to improve dispatch accuracy
	AvailableGpus int                                        // gpus available to mcad
	WhenAvailable time.Time                                  // when last computed
}

const (
	namespaceLabel = "mcad.my.domain/namespace" // owner namespace label for wrapped resources
	nameLabel      = "mcad.my.domain/name"      // owner name label for wrapped resources
	uidLabel       = "mcad.my.domain/uid"       // owner UID label for wrapped resources
	finalizer      = "mcad.my.domain/finalizer" // AppWrapper finalizer name
	nvidiaGpu      = "nvidia.com/gpu"           // GPU resource name
	specNodeName   = ".spec.nodeName"           // pod node name field
)

// PodCounts summarizes the status of the pods associated with one AppWrapper
type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile one AppWrapper
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile")

	// try to dispatch next appWrapper in queue order
	if req.Name == "*" {
		appWrapper, more, err := r.dispatchNext(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
		if appWrapper == nil {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // retry to dispatch later
		}
		if _, ok := r.Phases[appWrapper.UID]; ok && r.Phases[appWrapper.UID] != appWrapper.Status.Phase {
			// reconciler cache and our cache are out of sync
			delete(r.Phases, appWrapper.UID)                   // remove phase from our cache
			return ctrl.Result{}, errors.New("phase conflict") // force redo
		}
		// set dispatching timestamp and status
		appWrapper.Status.LastDispatchTime = metav1.Now()
		if _, err := r.updateStatus(ctx, appWrapper, mcadv1alpha1.Dispatching); err != nil {
			return ctrl.Result{}, err
		}
		if more {
			return ctrl.Result{Requeue: true}, nil // requeue to continue to dispatch queued appWrappers
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil // retry to dispatch later
	}

	appWrapper := &mcadv1alpha1.AppWrapper{}

	// get deep copy of AppWrapper object in reconciler cache
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	// cache AppWrapper phase because reconciler List calls may not reflect the most recent status
	if _, ok := r.Phases[appWrapper.UID]; !ok {
		// cache phase
		r.Phases[appWrapper.UID] = appWrapper.Status.Phase
	} else if r.Phases[appWrapper.UID] != appWrapper.Status.Phase {
		// reconciler cache and our cache are out of sync
		delete(r.Phases, appWrapper.UID)                   // remove phase from our cache
		return ctrl.Result{}, errors.New("phase conflict") // force redo
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
		delete(r.Phases, appWrapper.UID) // remove phase from cache
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
		WatchesMetadata(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map labelled pods to AppWrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*metav1.PartialObjectMetadata)
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
	r.Phases[appWrapper.UID] = phase // update cached phase
	activeAfter := isActivePhase(phase)
	if activeAfter && !activeBefore {
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

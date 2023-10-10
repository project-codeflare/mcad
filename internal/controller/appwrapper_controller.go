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
	"strconv"
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

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

// AppWrapperReconciler reconciles a AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Cache           map[types.UID]*CachedAppWrapper // cache AppWrapper updates for write/read consistency
	Events          chan event.GenericEvent         // event channel to trigger dispatch
	ClusterCapacity Weights                         // cluster capacity available to MCAD
	NextSync        time.Time                       // when to refresh cluster capacity
}

const (
	nameLabel      = "workload.codeflare.dev"           // owner name label for wrapped resources
	namespaceLabel = "workload.codeflare.dev/namespace" // owner namespace label for wrapped resources
	finalizer      = "workload.codeflare.dev/finalizer" // finalizer name
	nvidiaGpu      = "nvidia.com/gpu"                   // GPU resource name
	specNodeName   = ".spec.nodeName"                   // key to index pods based on node placement
)

// Structured logger
var mcadLog = ctrl.Log.WithName("MCAD")

func withAppWrapper(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) context.Context {
	return log.IntoContext(ctx, mcadLog.WithValues("namespace", appWrapper.Namespace, "name", appWrapper.Name, "uid", appWrapper.UID))
}

// Reconcile one AppWrapper or dispatch queued AppWrappers
// Normal reconciliations "namespace/name" implement all phase transitions except for Queued->Dispatching
// Queued->Dispatching transitions happen as part of a special "*/*" reconciliation
// In a "*/*" reconciliation, we iterate over queued AppWrappers in order, dispatching as many as we can
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// req == "*/*", dispatch queued AppWrappers
	if req.Namespace == "*" && req.Name == "*" {
		return r.dispatch(ctx)
	}

	// get deep copy of AppWrapper object in reconciler cache
	appWrapper := &mcadv1beta1.AppWrapper{}
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		r.triggerDispatch()
		return ctrl.Result{}, nil
	}

	// append appWrapper ID to logger
	ctx = withAppWrapper(ctx, appWrapper)

	// abort and requeue reconciliation if reconciler cache is stale
	if r.isStale(ctx, appWrapper) {
		return ctrl.Result{Requeue: true}, nil
	}

	// handle deletion
	if !appWrapper.DeletionTimestamp.IsZero() {
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			// deletion is pending, requeue reconciliation after delay
			return ctrl.Result{RequeueAfter: deletionDelay}, nil
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// remove AppWrapper from cache
		r.deleteCachedPhase(appWrapper)
		log.FromContext(ctx).Info("Deleted", "state", "Deleted")
		return ctrl.Result{}, nil
	}

	// handle other phases
	switch appWrapper.Status.Phase {
	case mcadv1beta1.Empty:
		// add finalizer
		if controllerutil.AddFinalizer(appWrapper, finalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status only after adding finalizer
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued)

	case mcadv1beta1.Queued, mcadv1beta1.Succeeded:
		r.triggerDispatch()
		return ctrl.Result{}, nil

	case mcadv1beta1.Dispatching:
		// create wrapped resources
		err, fatal := r.createResources(ctx, appWrapper)
		if err != nil {
			if fatal {
				return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, err.Error())
			}
			return r.requeueOrFail(ctx, appWrapper, err.Error())
		}
		// set running status only after successfully requesting the creation of all resources
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running)

	case mcadv1beta1.Running:
		// count AppWrapper pods
		counts, err := r.countPods(ctx, appWrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		// check pod count if dispatched for a while
		if isSlowDispatching(appWrapper) && counts.Running+counts.Succeeded < int(appWrapper.Spec.Scheduling.MinAvailable) {

			customMessage := "expected pods " + strconv.Itoa(int(appWrapper.Spec.Scheduling.MinAvailable)) + " but found pods " + strconv.Itoa(counts.Running+counts.Succeeded)
			// requeue or fail if max retries exhausted
			return r.requeueOrFail(ctx, appWrapper, customMessage)
		}
		// check for successful completion by looking at pods and wrapped resources
		success, err := r.isSuccessful(ctx, appWrapper, counts)
		if err != nil {
			return ctrl.Result{}, err
		}
		// set succeeded status if done
		if success {
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Succeeded)
		}
		// AppWrapper is healthy, requeue reconciliation after delay
		return ctrl.Result{RequeueAfter: runDelay}, nil

	case mcadv1beta1.Requeuing:
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			if !isSlowRequeuing(appWrapper) {
				// requeue reconciliation after delay
				return ctrl.Result{RequeueAfter: deletionDelay}, nil
			}
			// forcefully delete wrapped resources and pods
			r.forceDelete(ctx, appWrapper)
		}
		// reset status to queued
		appWrapper.Status.Restarts += 1
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued)
	}
	return ctrl.Result{}, nil
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
	// watch AppWrapper pods, watch events
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.AppWrapper{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		WatchesRawSource(&source.Channel{Source: r.Events}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// Map labelled pods to corresponding AppWrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if name, ok := pod.Labels[nameLabel]; ok {
		if namespace, ok := pod.Labels[namespaceLabel]; ok {
			if pod.Status.Phase == v1.PodSucceeded {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
			}
		}
	}
	return nil
}

// Update AppWrapper status
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, phase mcadv1beta1.AppWrapperPhase, reason ...string) (ctrl.Result, error) {
	// log transition
	now := metav1.Now()
	transition := mcadv1beta1.AppWrapperTransition{Time: now, Phase: phase}
	if len(reason) > 0 {
		transition.Reason = reason[0]
	}
	appWrapper.Status.Transitions = append(appWrapper.Status.Transitions, transition)
	appWrapper.Status.Phase = phase
	// update AppWrapper status in etcd, requeue reconciliation on failure
	if err := r.Status().Update(ctx, appWrapper); err != nil {
		return ctrl.Result{}, err
	}
	// cache AppWrapper status
	r.addCachedPhase(appWrapper)
	log.FromContext(ctx).Info(string(phase), "state", string(phase))
	return ctrl.Result{}, nil
}

// Set requeuing or failed status depending on restarts count
func (r *AppWrapperReconciler) requeueOrFail(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, reason string) (ctrl.Result, error) {
	if appWrapper.Status.Restarts < appWrapper.Spec.Scheduling.Requeuing.MaxNumRequeuings {
		appWrapper.Status.RequeueTimestamp = metav1.Now()
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Requeuing, reason)
	}
	return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, reason)
}

// Trigger dispatch by means of "*/*" request
func (r *AppWrapperReconciler) triggerDispatch() {
	select {
	case r.Events <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "*", Name: "*"}}}:
	default:
		// do not block if event is already in channel
	}
}

// Attempt to select and dispatch one appWrapper
func (r *AppWrapperReconciler) dispatch(ctx context.Context) (ctrl.Result, error) {
	for {
		// find next dispatch candidate according to priorities, precedence, and available resources
		appWrapper, err := r.selectForDispatch(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
		// if no AppWrapper can be dispatched, requeue reconciliation after delay
		if appWrapper == nil {
			return ctrl.Result{RequeueAfter: dispatchDelay}, nil
		}
		// append appWrapper ID to logger
		ctx := withAppWrapper(ctx, appWrapper)
		// abort and requeue reconciliation if reconciler cache is stale
		if r.isStale(ctx, appWrapper) {
			return ctrl.Result{Requeue: true}, nil
		}
		// check phase again to be extra safe
		if appWrapper.Status.Phase != mcadv1beta1.Queued {
			log.FromContext(ctx).Error(errors.New("not queued"), "Internal error")
			return ctrl.Result{Requeue: true}, nil
		}
		// set dispatching time and status
		appWrapper.Status.DispatchTimestamp = metav1.Now()
		if _, err := r.updateStatus(ctx, appWrapper, mcadv1beta1.Dispatching); err != nil {
			return ctrl.Result{}, err
		}
	}
}

// Is dispatching too slow?
func isSlowDispatching(appWrapper *mcadv1beta1.AppWrapper) bool {
	return metav1.Now().After(appWrapper.Status.DispatchTimestamp.Add(runningTimeout))
}

// Is requeuing too slow?
func isSlowRequeuing(appWrapper *mcadv1beta1.AppWrapper) bool {
	return metav1.Now().After(appWrapper.Status.RequeueTimestamp.Add(requeuingTimeout))
}

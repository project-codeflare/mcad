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
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// AppWrapperReconciler responsible for the portion of the lifecycle that happens on the scheduling cluster
type Dispatcher struct {
	AppWrapperReconciler
	Decisions          map[types.UID]*QueuingDecision // transient log of queuing decisions to enable recording in AppWrapper Status
	Events             chan event.GenericEvent        // event channel to trigger dispatch
	NextLoggedDispatch time.Time                      // when next to log dispatching decisions
}

// Reconcile one AppWrapper or dispatch queued AppWrappers during the dispatching phases of their lifecycle.
//
// Normal reconciliations "namespace/name" implement all phase transitions except for Queued->Dispatching
// Queued->Dispatching transitions happen as part of a special "*/*" reconciliation
// In a "*/*" reconciliation, we iterate over queued AppWrappers in order, dispatching as many as we can
//
//gocyclo:ignore
func (r *Dispatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		if r.MultiClusterMode {
			// TODO: Multicluster. This is where we remove the placement that maps
			// the appWrapper to the execution cluster. Only after the placement is
			// successfully removed will we proceed to removing the dispatch finalizer
			return ctrl.Result{}, fmt.Errorf("unimplemented multi-cluster synch path")
		} else {
			// wait for the runner to remove its finalizer before we remove ours
			if controllerutil.ContainsFinalizer(appWrapper, runnerFinalizer) {
				return ctrl.Result{RequeueAfter: deletionDelay}, nil
			}
		}

		// remove dispatcher finalizer
		if controllerutil.RemoveFinalizer(appWrapper, dispatchFinalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
			log.FromContext(ctx).Info("Deleted dispatcher finalizer")
		}
		// remove AppWrapper from cache
		r.deleteCachedAW(appWrapper)
		delete(r.Decisions, appWrapper.UID)
		return ctrl.Result{}, nil
	}

	// handle other states
	switch appWrapper.Status.State {
	case mcadv1beta1.Empty:
		// add finalizers
		var finalizerAdded = controllerutil.AddFinalizer(appWrapper, dispatchFinalizer)
		if !r.MultiClusterMode {
			finalizerAdded = controllerutil.AddFinalizer(appWrapper, runnerFinalizer) || finalizerAdded
		}
		if finalizerAdded {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued/idle status only after adding finalizers
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued, mcadv1beta1.Idle)

	case mcadv1beta1.Queued:
		// Propagate most recent queuing decision to AppWrapper's Queued Condition
		if decision, ok := r.Decisions[appWrapper.UID]; ok {
			meta.SetStatusCondition(&appWrapper.Status.Conditions, metav1.Condition{
				Type:    string(mcadv1beta1.Queued),
				Status:  metav1.ConditionTrue,
				Reason:  string(decision.reason),
				Message: decision.message,
			})
			if r.Status().Update(ctx, appWrapper) == nil {
				// If successfully propagated, remove from in memory map
				delete(r.Decisions, appWrapper.UID)
			}
		}

		if meta.FindStatusCondition(appWrapper.Status.Conditions, string(mcadv1beta1.Queued)) == nil {
			// Absence of Queued Condition strongly suggests AppWrapper is new; trigger dispatch and requeue
			r.triggerDispatch()
			return ctrl.Result{Requeue: true}, nil
		} else {
			return ctrl.Result{RequeueAfter: queuedDelay}, nil
		}

	case mcadv1beta1.Running:
		switch appWrapper.Status.Step {
		case mcadv1beta1.Dispatching:
			if r.MultiClusterMode {
				// TODO: Multicluster. This is where we create the placement that maps
				// the appWrapper to the execution cluster. Only if the placement creation is successful
				// will we do the update of the status to running/creating.
				return ctrl.Result{}, fmt.Errorf("unimplemented multi-cluster synch path")
			}
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Accepting)

		case mcadv1beta1.Deleted:
			if r.MultiClusterMode {
				// TODO: Multicluster. This is where we delete the placement that maps
				// the appWrapper to the execution cluster. Only after the placement is
				// completely deleted will we proceed to resetting the status to queued/idle
				return ctrl.Result{}, fmt.Errorf("unimplemented multi-cluster synch path")
			}

			// reset status to queued/idle
			appWrapper.Status.Restarts += 1
			appWrapper.Status.RequeueTimestamp = metav1.Now() // overwrite requeue decision time (runner clock) with completion time (dispatcher clock)
			msg := "Requeued by MCAD"
			if decision, ok := r.Decisions[appWrapper.UID]; ok && decision.reason == mcadv1beta1.QueuedRequeue {
				msg = fmt.Sprintf("Requeued because %s", decision.message)
			}
			meta.SetStatusCondition(&appWrapper.Status.Conditions, metav1.Condition{
				Type:    string(mcadv1beta1.Queued),
				Status:  metav1.ConditionTrue,
				Reason:  string(mcadv1beta1.QueuedRequeue),
				Message: msg,
			})
			res, err := r.updateStatus(ctx, appWrapper, mcadv1beta1.Queued, mcadv1beta1.Idle)
			if err == nil {
				delete(r.Decisions, appWrapper.UID)
				r.triggerDispatch()
			}
			return res, err
		}

	case mcadv1beta1.Succeeded:
		switch appWrapper.Status.Step {
		case mcadv1beta1.Idle:
			r.triggerDispatch()
			return ctrl.Result{}, nil
		}

	case mcadv1beta1.Failed:
		switch appWrapper.Status.Step {
		case mcadv1beta1.Deleted:
			if r.MultiClusterMode {
				// TODO: Multicluster. This is where we delete the placement that maps
				// the appWrapper to the execution cluster. Only after the placement is
				// completely deleted will we proceed to resetting the status to failed/idle
				return ctrl.Result{}, fmt.Errorf("unimplemented multi-cluster synch path")
			}
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, mcadv1beta1.Idle)

		case mcadv1beta1.Idle:
			r.triggerDispatch()
			return ctrl.Result{}, nil
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Dispatcher) SetupWithManager(mgr ctrl.Manager) error {
	// initialize periodic dispatch invocation
	r.triggerDispatch()
	// watch AppWrappers and dispatch events
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.AppWrapper{}).
		WatchesRawSource(&source.Channel{Source: r.Events}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// Trigger dispatch by means of "*/*" request
func (r *Dispatcher) triggerDispatch() {
	select {
	case r.Events <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "*", Name: "*"}}}:
	default:
		// do not block if event is already in channel
	}
}

// Attempt to select and dispatch appWrappers until either capacity is exhausted or no candidates remain
func (r *Dispatcher) dispatch(ctx context.Context) (ctrl.Result, error) {
	// track quota allocation to AppWrappers during a dispatching cycle;
	// used in only one cycle, does not carry from cycle to cycle
	quotaTracker := NewQuotaTracker()
	if weightsPairMap, err := r.getUnadmittedAppWrappersWeights(ctx); err == nil {
		quotaTracker.Init(weightsPairMap)
	}
	// find dispatch candidates according to priorities, precedence, and available resources
	selectedAppWrappers, err := r.selectForDispatch(ctx, quotaTracker)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Dispatch one by one until either exhausted candidates or hit an error
	for _, appWrapper := range selectedAppWrappers {
		// append appWrapper ID to logger
		ctx := withAppWrapper(ctx, appWrapper)
		// abort and requeue reconciliation if reconciler cache is stale
		if r.isStale(ctx, appWrapper) {
			return ctrl.Result{Requeue: true}, nil
		}
		// check state again to be extra safe
		if appWrapper.Status.State != mcadv1beta1.Queued {
			log.FromContext(ctx).Error(errors.New("not queued"), "Internal error")
			return ctrl.Result{Requeue: true}, nil
		}
		meta.SetStatusCondition(&appWrapper.Status.Conditions, metav1.Condition{
			Type:    string(mcadv1beta1.Queued),
			Status:  metav1.ConditionFalse,
			Reason:  string(mcadv1beta1.QueuedDispatch),
			Message: "Selected for dispatch",
		})
		if _, err := r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Dispatching); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: dispatchDelay}, nil
}

// Calculate resource demands of appWrappers that have been dispatched but haven't
// passed through ResourceQuota admission controller yet (approximated by resources not created yet)
func (r *Dispatcher) getUnadmittedAppWrappersWeights(ctx context.Context) (map[string]*WeightsPair, error) {
	appWrappers := &mcadv1beta1.AppWrapperList{}
	if err := r.List(ctx, appWrappers, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	weightsPairMap := make(map[string]*WeightsPair)
	for _, appWrapper := range appWrappers.Items {
		phase, step := r.getCachedAW(&appWrapper)
		if phase == mcadv1beta1.Running &&
			step == mcadv1beta1.Idle || step == mcadv1beta1.Creating || step == mcadv1beta1.Created {
			namespace := appWrapper.GetNamespace()
			weightsPair := weightsPairMap[namespace]
			if weightsPair == nil {
				weightsPair = NewWeightsPair(Weights{}, Weights{})
			}
			weightsPair.Add(getWeightsPairForAppWrapper(&appWrapper))

			// subtract weights for admitted (created) pods for this appWrapper
			// (already accounted for in the used status of the resourceQuota)
			pods := &v1.PodList{}
			if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
				client.MatchingLabels{namespaceLabel: namespace, nameLabel: appWrapper.Name}); err == nil {
				createdPodsWeightsPair := &WeightsPair{requests: Weights{}, limits: Weights{}}
				for _, pod := range pods.Items {
					createdPodsWeightsPair.Add(NewWeightsPairForPod(&pod))
				}
				weightsPair.Sub(createdPodsWeightsPair)
			}
			nonNegativeWeightsPair := NewWeightsPair(Weights{}, Weights{})
			nonNegativeWeightsPair.Max(weightsPair)
			weightsPairMap[namespace] = nonNegativeWeightsPair
		}
	}
	return weightsPairMap, nil
}

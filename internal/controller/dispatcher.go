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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	ksv1alpha1 "github.com/kubestellar/kubestellar/api/control/v1alpha1"
	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// AppWrapperReconciler responsible for the portion of the lifecycle that happens on the scheduling cluster
type Dispatcher struct {
	AppWrapperReconciler
	Decisions          map[types.UID]*QueuingDecision // transient log of queuing decisions to enable recording in AppWrapper Status
	Events             chan event.GenericEvent        // event channel to trigger dispatch
	NextLoggedDispatch time.Time                      // when next to log dispatching decisions
}

const (
	ksLabelLocationGroupKey = "location-group"
	ksLabelLocationGroup    = "edge"
	ksLabelClusterNameKey   = "name"
)

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
			if err := r.deleteBindingPolicy(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
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
				// Serialize current status to an annotation so it can be mirrored in the execution cluster
				pickledStatus, err := json.Marshal(appWrapper.Status)
				if err != nil {
					return ctrl.Result{}, err
				}
				metav1.SetMetaDataAnnotation(&appWrapper.ObjectMeta, serializedStatusKey, string(pickledStatus))
				if err := r.Update(ctx, appWrapper); err != nil {
					return ctrl.Result{}, err
				}

				if err := r.createBindingPolicy(ctx, appWrapper); err != nil {
					log.FromContext(ctx).Error(err, "Error while creating BindingPolicy object")
					return ctrl.Result{}, err
				}
			}
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Accepting)

		case mcadv1beta1.Deleted:
			if r.MultiClusterMode {
				if err := r.deleteBindingPolicy(ctx, appWrapper); err != nil {
					return ctrl.Result{}, err
				}
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
				if err := r.deleteBindingPolicy(ctx, appWrapper); err != nil {
					return ctrl.Result{}, err
				}
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
	if weightsPairMap, err := r.getUnadmittedPodsWeights(ctx); err == nil {
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

func (r *Dispatcher) createBindingPolicy(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) error {
	bindingPolicy := ksv1alpha1.BindingPolicy{}
	namespacedName := types.NamespacedName{
		Name: bindingPolicyName(appWrapper),
	}
	if err := r.Get(ctx, namespacedName, &bindingPolicy); err != nil {
		// create BindingPolicy object per appwrapper
		if apierrors.IsNotFound(err) {

			if appWrapper.Labels != nil && appWrapper.Labels[assignedClusterLabel] != "" {

				bindingPolicy = ksv1alpha1.BindingPolicy{
					Spec: ksv1alpha1.BindingPolicySpec{
						ClusterSelectors: []metav1.LabelSelector{
							{
								MatchLabels: map[string]string{
									ksLabelLocationGroupKey: ksLabelLocationGroup,
									ksLabelClusterNameKey:   appWrapper.Labels[assignedClusterLabel],
								},
							},
						},
						Downsync: []ksv1alpha1.DownsyncObjectTest{
							{
								Namespaces: []string{
									appWrapper.Namespace,
								},
								ObjectNames: []string{
									appWrapper.Name,
								},

								ObjectSelectors: []metav1.LabelSelector{
									{
										MatchLabels: map[string]string{
											assignedClusterLabel: appWrapper.Labels[assignedClusterLabel],
										},
									},
								},
							},
						},
						// turn the flag on so that kubestellar updates status of the appwrapper
						WantSingletonReportedState: true,
					},
				}
				bindingPolicy.Name = bindingPolicyName(appWrapper)
				if err := r.Create(ctx, &bindingPolicy); err != nil {
					if apierrors.IsAlreadyExists(err) {
						return nil
					}
					return err
				}
				// BindingPolicy created
				return nil
			} else {
				log.FromContext(ctx).Error(errors.New("cluster assignment label is missing from the AppWrapper"), "Add label", assignedClusterLabel)
			}
		} else {
			return err
		}
	}
	return nil
}
func bindingPolicyName(appWrapper *mcadv1beta1.AppWrapper) string {
	return appWrapper.Name + "-" + appWrapper.Namespace
}
func (r *Dispatcher) deleteBindingPolicy(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) error {
	bindingPolicy := ksv1alpha1.BindingPolicy{}
	namespacedName := types.NamespacedName{
		Name: bindingPolicyName(appWrapper),
	}
	if err := r.Get(ctx, namespacedName, &bindingPolicy); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		log.FromContext(ctx).Error(errors.New("error fetching bindingPolicy object"), namespacedName.Name)
		return err
	}
	if err := r.Delete(ctx, &bindingPolicy); err != nil {
		log.FromContext(ctx).Error(errors.New("unable to delete bindingPolicy object"), namespacedName.Name)
		return err
	}
	log.FromContext(ctx).Info("bindingPolicy object deleted", namespacedName.Name)
	return nil
}

// Calculate resource demands of pods for appWrappers that have been dispatched but haven't
// passed through ResourceQuota admission controller yet (approximated by resources not created yet)
func (r *Dispatcher) getUnadmittedPodsWeights(ctx context.Context) (map[string]*WeightsPair, error) {
	appWrappers := &mcadv1beta1.AppWrapperList{}
	if err := r.List(ctx, appWrappers, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	weightsPairMap := make(map[string]*WeightsPair)
	for _, appWrapper := range appWrappers.Items {
		_, step := r.getCachedAW(&appWrapper)
		if step != mcadv1beta1.Idle {
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

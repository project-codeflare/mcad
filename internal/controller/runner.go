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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcadv1beta1 "github.com/tardieu/mcad/api/v1beta1"
)

type Runner struct {
	AppWrapperReconciler
}

func (r *Runner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// get deep copy of AppWrapper object in reconciler cache
	appWrapper := &mcadv1beta1.AppWrapper{}
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	// abort and requeue reconciliation if reconciler cache is stale
	if err := r.checkCachedPhase(appWrapper); err != nil {
		return ctrl.Result{}, err
	}

	// handle deletion
	if !appWrapper.DeletionTimestamp.IsZero() {
		if appWrapper.Status.RunnerStatus.Phase != mcadv1beta1.Empty {
			// delete wrapped resources
			if r.deleteResources(ctx, appWrapper) != 0 {
				// requeue reconciliation
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			// set empty status
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Empty)
		}
		return ctrl.Result{}, nil
	}

	// propagate failed phase from dispatcher to runner
	if appWrapper.Spec.DispatcherStatus.Phase == mcadv1beta1.Failed {
		if appWrapper.Status.RunnerStatus.Phase != mcadv1beta1.Failed {
			// set failed status and reason
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, appWrapper.Spec.DispatcherStatus.Transitions[len(appWrapper.Spec.DispatcherStatus.Transitions)-1].Reason)
		}
		return ctrl.Result{}, nil
	}

	// propagate requeuing phase from dispatcher to runner
	if appWrapper.Spec.DispatcherStatus.Phase == mcadv1beta1.Requeuing &&
		appWrapper.Status.RunnerStatus.Phase != mcadv1beta1.Requeuing &&
		appWrapper.Status.RunnerStatus.Phase != mcadv1beta1.Empty {
		appWrapper.Status.RunnerStatus.LastRequeuingTime = metav1.Now()
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Requeuing)
	}

	// handle other phases
	switch appWrapper.Status.RunnerStatus.Phase {
	case mcadv1beta1.Requeuing:
		// delete wrapped resources
		if r.deleteResources(ctx, appWrapper) != 0 {
			if !isSlowRequeuing(appWrapper) {
				// requeue reconciliation
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			r.forceDelete(ctx, appWrapper)
		}
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Empty)

	case mcadv1beta1.Dispatching:
		if appWrapper.Spec.DispatcherStatus.Phase == mcadv1beta1.Running {
			// parse wrapped resources
			objects, err := parseResources(appWrapper)
			if err != nil {
				return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, "resource parsing error")
			}
			// create wrapped resources
			if err := r.createResources(ctx, objects); err != nil {
				return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, "resource creation error")
			}
			// set running status only after successfully requesting the creation of all resources
			appWrapper.Status.RunnerStatus.LastRunningTime = metav1.Now()
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running)
		}

	case mcadv1beta1.Running:
		// check AppWrapper health
		counts, err := r.countPods(ctx, appWrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		if isSlowRunning(appWrapper) && counts.Running < int(appWrapper.Spec.Scheduling.MinAvailable) {
			// set errored status
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, "too few running pods")
		}
		// check for successful completion by looking at pods and wrapped resources
		done, err := r.checkCompletion(ctx, appWrapper, counts)
		if err != nil {
			return ctrl.Result{}, err
		}
		if done {
			// set succeeded status
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Succeeded)
		}
		return ctrl.Result{RequeueAfter: runDelay}, nil // check again soon

	case mcadv1beta1.Empty:
		if appWrapper.Spec.DispatcherStatus.Phase == mcadv1beta1.Dispatching {
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Dispatching)
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Runner) SetupWithManager(mgr ctrl.Manager) error {
	// index pods with nodeName key
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, specNodeName, func(obj client.Object) []string {
		pod := obj.(*v1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return err
	}
	// watch pods
	return ctrl.NewControllerManagedBy(mgr).
		Named("runner").
		For(&mcadv1beta1.AppWrapper{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map labelled pods to corresponding AppWrappers
func (r *Runner) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
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
func (r *Runner) updateStatus(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, phase mcadv1beta1.AppWrapperPhase, reason ...string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	now := metav1.Now()
	// log transition
	transition := mcadv1beta1.AppWrapperTransition{Time: now, Phase: phase}
	if len(reason) > 0 {
		transition.Reason = reason[0]
	}
	appWrapper.Status.RunnerStatus.Transitions = append(appWrapper.Status.RunnerStatus.Transitions, transition)
	appWrapper.Status.RunnerStatus.Phase = phase
	if err := r.Status().Update(ctx, appWrapper); err != nil {
		return ctrl.Result{}, err // etcd update failed, abort and requeue reconciliation
	}
	if false {
		log.Info(string(phase))
	}
	// cache AppWrapper status
	r.addCachedPhase(appWrapper)
	return ctrl.Result{}, nil
}

// Is running too slow?
func isSlowRunning(appWrapper *mcadv1beta1.AppWrapper) bool {
	return metav1.Now().After(appWrapper.Status.RunnerStatus.LastRunningTime.Add(runningTimeout))
}

// Is requeuing too slow?
func isSlowRequeuing(appWrapper *mcadv1beta1.AppWrapper) bool {
	return metav1.Now().After(appWrapper.Status.RunnerStatus.LastRequeuingTime.Add(time.Minute))
}

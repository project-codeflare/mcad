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
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// AppWrapperReconciler responsible for the portion of the lifecycle that happens on the execution cluster
type Runner struct {
	AppWrapperReconciler
}

// permission to edit appwrappers

//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers/finalizers,verbs=update

// permission to edit wrapped resources: pods, services, jobs, podgroups

//+kubebuilder:rbac:groups="",resources=pods;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=scheduling.sigs.k8s.io,resources=podgroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=scheduling.x-k8s.io,resources=podgroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kubeflow.org,resources=pytorchjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.ray.io,resources=rayclusters,verbs=get;list;watch;create;update;patch;delete

// permission to view nodes

//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile one Running AppWrapper on an execution cluster and monitor health of its execution resources.
//
//gocyclo:ignore
func (r *Runner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// get deep copy of AppWrapper object in reconciler cache
	appWrapper := &mcadv1beta1.AppWrapper{}
	if err := r.Get(ctx, req.NamespacedName, appWrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
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
		if !r.deleteResources(ctx, appWrapper, *appWrapper.DeletionTimestamp) {
			// requeue reconciliation after delay
			return ctrl.Result{RequeueAfter: deletionDelay}, nil
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(appWrapper, runnerFinalizer) {
			if err := r.Update(ctx, appWrapper); err != nil {
				return ctrl.Result{}, err
			}
			log.FromContext(ctx).Info("Deleted runner finalizer")
		}
		// remove AppWrapper from cache
		r.deleteCachedAW(appWrapper)
		return ctrl.Result{}, nil
	}

	// handle state transitions that occur on the execution cluster
	switch appWrapper.Status.State {
	case mcadv1beta1.Running:
		switch appWrapper.Status.Step {

		case mcadv1beta1.Creating:
			if r.MultiClusterMode {
				// add runner finalizer to this copy of the object
				if controllerutil.AddFinalizer(appWrapper, runnerFinalizer) {
					if err := r.Update(ctx, appWrapper); err != nil {
						return ctrl.Result{}, err
					}
				}
			}

			// create wrapped resources
			if err, fatal := r.createResources(ctx, appWrapper); err != nil {
				return r.requeueOrFail(ctx, appWrapper, fatal, err.Error())
			}
			// set running/created status only after successfully requesting the creation of all resources
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Created)

		case mcadv1beta1.Created:
			// count AppWrapper pods
			counts, err := r.countPods(ctx, appWrapper)
			if err != nil {
				return ctrl.Result{}, err
			}
			// check for successful completion by looking at pods and wrapped resources
			success, err := r.isSuccessful(ctx, appWrapper, counts)
			if err != nil {
				return ctrl.Result{}, err
			}
			// set succeeded/idle status if done
			if success {
				return r.updateStatus(ctx, appWrapper, mcadv1beta1.Succeeded, mcadv1beta1.Idle)
			}
			// check pod count if dispatched for a while
			minAvailable := appWrapper.Spec.Scheduling.MinAvailable
			if minAvailable == 0 {
				minAvailable = 1 // default to expecting 1 running pod
			}
			if metav1.Now().After(appWrapper.Status.DispatchTimestamp.Add(time.Duration(appWrapper.Spec.Scheduling.Requeuing.TimeInSeconds)*time.Second)) &&
				counts.Running+counts.Succeeded < int(minAvailable) {
				customMessage := "expected pods " + strconv.Itoa(int(minAvailable)) + " but found pods " + strconv.Itoa(counts.Running+counts.Succeeded)
				// requeue or fail if max retries exhausted with custom error message
				return r.requeueOrFail(ctx, appWrapper, false, customMessage)
			}
			// AppWrapper is healthy, requeue reconciliation after delay
			return ctrl.Result{RequeueAfter: runDelay}, nil

		case mcadv1beta1.Deleting:
			// delete wrapped resources
			if !r.deleteResources(ctx, appWrapper, appWrapper.Status.RequeueTimestamp) {
				// requeue reconciliation after delay
				return ctrl.Result{RequeueAfter: deletionDelay}, nil
			}
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Deleted)
		}

	case mcadv1beta1.Failed:
		switch appWrapper.Status.Step {
		case mcadv1beta1.Deleting:
			// delete wrapped resources
			if !r.deleteResources(ctx, appWrapper, appWrapper.Status.RequeueTimestamp) {
				// requeue reconciliation after delay
				return ctrl.Result{RequeueAfter: deletionDelay}, nil
			}
			// set status to failed/deleted
			return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, mcadv1beta1.Deleted)
		}
	}

	return ctrl.Result{}, nil
}

// Set requeuing or failed status depending on error, configuration, and restarts count
func (r *AppWrapperReconciler) requeueOrFail(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, fatal bool, reason string) (ctrl.Result, error) {
	if appWrapper.Spec.Scheduling.MinAvailable < 0 {
		// set failed status and leave resources as is
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, appWrapper.Status.Step, reason)
	} else if fatal || appWrapper.Spec.Scheduling.Requeuing.MaxNumRequeuings > 0 && appWrapper.Status.Restarts >= appWrapper.Spec.Scheduling.Requeuing.MaxNumRequeuings {
		// set failed/deleting status (request deletion of wrapped resources)
		appWrapper.Status.RequeueTimestamp = metav1.Now()
		return r.updateStatus(ctx, appWrapper, mcadv1beta1.Failed, mcadv1beta1.Deleting, reason)
	}
	// start process of requeueing AppWrapper by requesting the deletion of wrapped resources
	appWrapper.Status.RequeueTimestamp = metav1.Now()
	return r.updateStatus(ctx, appWrapper, mcadv1beta1.Running, mcadv1beta1.Deleting, reason)
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

// Map labelled jobs to corresponding AppWrappers
func (r *Runner) jobMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	job := obj.(*batchv1.Job)
	if name, ok := job.Labels[nameLabel]; ok {
		if namespace, ok := job.Labels[namespaceLabel]; ok {
			if !job.Status.CompletionTime.IsZero() {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Runner) SetupWithManager(mgr ctrl.Manager) error {
	// watch AppWrappers, pods, jobs
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1beta1.AppWrapper{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Watches(&batchv1.Job{}, handler.EnqueueRequestsFromMapFunc(r.jobMapFunc)).
		Complete(r)
}

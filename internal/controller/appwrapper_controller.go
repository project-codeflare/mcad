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
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// AppWrapperReconciler reconciles an AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	label        = "mcad.my.domain/AppWrapper" // label injected in every wrapped resource
	finalizer    = "mcad.my.domain/finalizer"  // AppWrapper finalizer name
	nvidiaGpu    = "nvidia.com/gpu"            // GPU resource name
	specNodeName = ".spec.nodeName"            // pod node name field
)

// PodCounts summarizes the status of the pods associated with one AppWrapper
type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

//+kubebuilder:rbac:groups=mcad.my.domain,resources=AppWrappers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcad.my.domain,resources=AppWrappers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcad.my.domain,resources=AppWrappers/finalizers,verbs=update

// Reconcile one AppWrapper
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile")

	appwrapper := &mcadv1alpha1.AppWrapper{}

	if err := r.Get(ctx, req.NamespacedName, appwrapper); err != nil {
		// no such AppWrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	// deletion requested
	if !appwrapper.ObjectMeta.DeletionTimestamp.IsZero() && appwrapper.Status.Phase != mcadv1alpha1.Terminating {
		return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Terminating)
	}

	switch appwrapper.Status.Phase {
	case mcadv1alpha1.Succeeded, mcadv1alpha1.Failed:
		// nothing to reconcile
		return ctrl.Result{}, nil

	case mcadv1alpha1.Terminating:
		// delete wrapped resources
		if r.deleteResources(ctx, appwrapper) != 0 {
			if isSlowDeletion(appwrapper) {
				log.Error(nil, "Resource deletion timeout")
			} else {
				return ctrl.Result{RequeueAfter: time.Minute}, nil // requeue
			}
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(appwrapper, finalizer) {
			if err := r.Update(ctx, appwrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil

	case mcadv1alpha1.Requeuing:
		// delete wrapped resources
		if r.deleteResources(ctx, appwrapper) != 0 {
			if isSlowRequeuing(appwrapper) {
				log.Error(nil, "Resource deletion timeout")
			} else {
				return ctrl.Result{RequeueAfter: time.Minute}, nil // requeue
			}
		}
		// update status to queued
		return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Queued)

	case mcadv1alpha1.Queued:
		// check if AppWrapper fits available resources
		shouldDispatch, err := r.shouldDispatch(ctx, appwrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		if shouldDispatch {
			// set dispatching status
			appwrapper.Status.LastDispatchTime = metav1.Now()
			return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Dispatching)
		}
		// if not, retry after a delay
		return ctrl.Result{RequeueAfter: time.Minute}, nil

	case mcadv1alpha1.Dispatching:
		// dispatching is taking too long?
		if isSlowDispatch(appwrapper) {
			// set requeuing or failed status
			if appwrapper.Status.Requeued < appwrapper.Spec.MaxRetries {
				appwrapper.Status.Requeued += 1
				appwrapper.Status.LastRequeuingTime = metav1.Now()
				return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Requeuing)
			}
			return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Failed)
		}
		// create wrapped resources
		objects, err := r.parseResources(appwrapper)
		if err != nil {
			log.Error(err, "Resource parsing error during creation")
			return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Failed)
		}
		// create wrapped resources
		if err := r.createResources(ctx, objects); err != nil {
			return ctrl.Result{}, err
		}
		// set running status only after successfully requesting the creation of all resources
		return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Running)

	case mcadv1alpha1.Running:
		// check AppWrapper health
		counts, err := r.monitorPods(ctx, appwrapper)
		if err != nil {
			return ctrl.Result{}, err
		}
		slow := isSlowDispatch(appwrapper)
		if counts.Failed > 0 || slow && (counts.Other > 0 || counts.Running < int(appwrapper.Spec.MinPods)) {
			// set requeuing or failed status
			if appwrapper.Status.Requeued < appwrapper.Spec.MaxRetries {
				appwrapper.Status.Requeued += 1
				appwrapper.Status.LastRequeuingTime = metav1.Now()
				return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Requeuing)
			}
			return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Failed)
		}
		if appwrapper.Spec.MinPods > 0 && counts.Succeeded >= int(appwrapper.Spec.MinPods) &&
			counts.Running == 0 && counts.Other == 0 {
			// set succeeded status
			return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Succeeded)
		}
		if !slow {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // check soon
		}
		return ctrl.Result{}, nil

	default: // empty phase
		// add finalizer
		if controllerutil.AddFinalizer(appwrapper, finalizer) {
			if err := r.Update(ctx, appwrapper); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status only after adding finalizer
		return ctrl.Result{}, r.updateStatus(ctx, appwrapper, mcadv1alpha1.Queued)
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
	// watch pods in addition to AppWrappers
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1alpha1.AppWrapper{}).
		WatchesMetadata(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map labelled pods to AppWrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*metav1.PartialObjectMetadata)
	if appwrapper, ok := pod.ObjectMeta.Labels[label]; ok {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: appwrapper}}}
	}
	return nil
}

// Update AppWrapper status
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, appwrapper *mcadv1alpha1.AppWrapper, phase mcadv1alpha1.AppWrapperPhase) error {
	log := log.FromContext(ctx)
	now := metav1.Now()
	if phase == mcadv1alpha1.Dispatching {
		now = appwrapper.Status.LastDispatchTime // ensure timestamps are consistent
	} else if phase == mcadv1alpha1.Requeuing {
		now = appwrapper.Status.LastRequeuingTime // ensure timestamps are consistent
	}
	// log transition as condition
	condition := mcadv1alpha1.AppWrapperCondition{LastTransitionTime: now, Reason: string(phase)}
	appwrapper.Status.Conditions = append(appwrapper.Status.Conditions, condition)
	appwrapper.Status.Phase = phase
	if err := r.Status().Update(ctx, appwrapper); err != nil {
		return err
	}
	log.Info(string(phase))
	return nil
}

// Are resources reserved in this phase
func isActivePhase(phase mcadv1alpha1.AppWrapperPhase) bool {
	switch phase {
	case mcadv1alpha1.Dispatching, mcadv1alpha1.Running, mcadv1alpha1.Failed, mcadv1alpha1.Terminating, mcadv1alpha1.Requeuing:
		return true
	default:
		return false
	}
}

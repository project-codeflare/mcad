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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mcadv1alpha1 "tardieu/mcad/api/v1alpha1"
)

// AppWrapperReconciler reconciles a AppWrapper object
type AppWrapperReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const label = "mcad.my.domain/appwrapper"
const finalizer = "mcad.my.domain/finalizer"

//+kubebuilder:rbac:groups=mcad.my.domain,resources=appwrappers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mcad.my.domain,resources=appwrappers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mcad.my.domain,resources=appwrappers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AppWrapper object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *AppWrapperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile")

	var aw mcadv1alpha1.AppWrapper

	if err := r.Get(ctx, req.NamespacedName, &aw); err != nil {
		return ctrl.Result{}, nil // no such appwrapper, nothing to reconcile, not an error
	}

	// deletion has been requested, check this first!
	if !aw.ObjectMeta.DeletionTimestamp.IsZero() {
		// set terminating status
		if aw.Status.Phase != "Terminating" {
			aw.Status.Phase = "Terminating"
			if err := r.Status().Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Terminating")
			return ctrl.Result{}, nil
		}
		// request deletion of wrapped resources and confirm deletion
		finished, err := r.deleteResources(ctx, aw)
		if err != nil {
			return ctrl.Result{}, err
		}
		// if deletion of wrapped resources is complete delete finalizer
		// TODO: maybe wait for the deletion of all labeled resources instead, could increase API server load
		if finished && controllerutil.RemoveFinalizer(&aw, finalizer) {
			if err := r.Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
		}
		// we do not monitor wrapped resources in general, only pods
		// we need to requeue after a delay to ensure we do not get stuck in this state
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if aw.Status.Phase == "Completed" || aw.Status.Phase == "Failed" {
		// nothing to reconcile
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "Dispatching" {
		if metav1.Now().After(aw.Status.LastDispatchTime.Add(2 * time.Minute)) {
			aw.Status.Phase = "Failed"
			if err := r.Status().Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Failed")
			return ctrl.Result{}, nil
		}
		// create appwrapper resources
		if err := r.createResources(ctx, aw); err != nil {
			return ctrl.Result{}, err
		}
		// set running status only after successfully requesting the creation of all resources
		aw.Status.Phase = "Running"
		if err := r.Status().Update(ctx, &aw); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Running")
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "Running" {
		// list matching pods
		var pods v1.PodList
		if err := r.List(ctx, &pods, client.InNamespace(req.Namespace), client.MatchingLabels{label: req.Name}); err != nil {
			return ctrl.Result{}, err
		}
		// count pods
		succeeded := 0
		running := 0
		failed := 0
		other := 0
		for _, pod := range pods.Items {
			switch pod.Status.Phase {
			case "Succeeded":
				succeeded += 1
			case "Running":
				running += 1
			case "Failed":
				failed += 1
			default:
				other += 1
			}
		}
		if failed > 0 || other > 0 && metav1.Now().After(aw.Status.LastDispatchTime.Add(2*time.Minute)) {
			// set failed status
			aw.Status.Phase = "Failed"
			if err := r.Status().Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Failed")
			return ctrl.Result{}, nil
		}
		if succeeded >= int(aw.Spec.Pods) && running == 0 && other == 0 {
			// set completed status
			aw.Status.Phase = "Completed"
			if err := r.Status().Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Completed")
			return ctrl.Result{}, nil
		}
		if !metav1.Now().After(aw.Status.LastDispatchTime.Add(2 * time.Minute)) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "" {
		// add finalizer
		if controllerutil.AddFinalizer(&aw, finalizer) {
			if err := r.Update(ctx, &aw); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status
		aw.Status.Phase = "Queued"
		if err := r.Status().Update(ctx, &aw); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Queued")
		return ctrl.Result{}, nil
	}

	// status == "Queued"

	// list all appwrappers
	var aws mcadv1alpha1.AppWrapperList
	if err := r.List(ctx, &aws); err != nil {
		return ctrl.Result{}, err
	}

	// compute available gpu capacity at desired priority level
	var gpus int32 = 40 // cluster capacity available to mcad,TODO should be dynamic
	// this is cluster capacity minus gpu usage by things other than appwrappers
	for _, a := range aws.Items {
		if (slices.Contains([]string{"Dispatching", "Running", "Terminating", "Failed"}, a.Status.Phase)) &&
			a.Spec.Priority >= aw.Spec.Priority {
			gpus -= a.Spec.Gpus
		}
	}

	// not enough gpus available
	if aw.Spec.Gpus > gpus {
		return ctrl.Result{RequeueAfter: time.Minute}, nil // TODO retry delay?
	}

	// set dispatching status
	aw.Status.Phase = "Dispatching"
	aw.Status.LastDispatchTime = metav1.Now()
	if err := r.Status().Update(ctx, &aw); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Dispatching")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppWrapperReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// watch pods in addition to appwrappers
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1alpha1.AppWrapper{}).Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map pods to appwrappers using labels
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if aw, ok := pod.ObjectMeta.Labels[label]; ok {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: aw}}}
	}
	return nil
}

// Create appwrapper resources
func (r *AppWrapperReconciler) createResources(ctx context.Context, aw mcadv1alpha1.AppWrapper) error {
	for _, resource := range aw.Spec.Resources {
		raw, _, err := unstructured.UnstructuredJSONScheme.Decode(resource.Raw, nil, nil)
		if err != nil {
			return err
		}
		obj := raw.(client.Object)
		namespaced, err := r.IsObjectNamespaced(obj)
		if err != nil {
			return err
		}
		if namespaced {
			obj.SetNamespace(aw.ObjectMeta.Namespace) // override namespace
		}
		obj.SetLabels(map[string]string{label: aw.ObjectMeta.Name}) // add appwrapper label
		if err := r.Create(ctx, obj); err != nil {
			if !errors.IsAlreadyExists(err) { // ignore existing resources
				return err
			}
		}
	}
	return nil
}

// Delete wrapped resources
func (r *AppWrapperReconciler) deleteResources(ctx context.Context, aw mcadv1alpha1.AppWrapper) (bool, error) {
	notfound := 0
	for _, resource := range aw.Spec.Resources {
		raw, _, err := unstructured.UnstructuredJSONScheme.Decode(resource.Raw, nil, nil)
		if err != nil {
			return false, err
		}
		obj := raw.(client.Object)
		namespaced, err := r.IsObjectNamespaced(obj)
		if err != nil {
			return false, err
		}
		if namespaced {
			obj.SetNamespace(aw.ObjectMeta.Namespace) // override namespace
		}
		obj.SetLabels(map[string]string{label: aw.ObjectMeta.Name}) // add appwrapper label
		client.ObjectKeyFromObject(obj)
		if err := r.Delete(ctx, obj); err != nil {
			if !errors.IsNotFound(err) { // ignore missing resources
				return false, err
			}
			notfound += 1
		}
		// do not increment notfound immediately after deletion as resource could be terminating still
	}
	return notfound == len(aw.Spec.Resources), nil
}

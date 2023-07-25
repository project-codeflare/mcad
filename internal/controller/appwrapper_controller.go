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

type PodCounts struct {
	Failed    int
	Other     int
	Running   int
	Succeeded int
}

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

	aw := &mcadv1alpha1.AppWrapper{}

	if err := r.Get(ctx, req.NamespacedName, aw); err != nil {
		// no such appwrapper, nothing to reconcile, not an error
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "Terminating" {
		// count wrapped resources
		count, err := r.countResources(ctx, aw)
		if err != nil {
			return ctrl.Result{}, err
		}
		// requeue if deletion still pending
		if count != 0 {
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		// remove finalizer
		if controllerutil.RemoveFinalizer(aw, finalizer) {
			if err := r.Update(ctx, aw); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !aw.ObjectMeta.DeletionTimestamp.IsZero() {
		// request deletion of wrapped resources
		if err := r.deleteResources(ctx, aw); err != nil {
			return ctrl.Result{}, err
		}
		// set terminating status only after successfully requesting the deletion of all resources
		return ctrl.Result{}, r.updateStatus(ctx, aw, "Terminating")
	}

	if aw.Status.Phase == "Completed" || aw.Status.Phase == "Failed" {
		// nothing to reconcile
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "Dispatching" {
		// dispatching is taking too long?
		if slowDispatch(aw) {
			return ctrl.Result{}, r.updateStatus(ctx, aw, "Failed")
		}
		// create wrapped resources
		if err := r.createResources(ctx, aw); err != nil {
			return ctrl.Result{}, err
		}
		// set running status only after successfully requesting the creation of all resources
		return ctrl.Result{}, r.updateStatus(ctx, aw, "Running")
	}

	if aw.Status.Phase == "Running" {
		// check appwrapper health
		counts, err := r.monitorPods(ctx, aw)
		if err != nil {
			return ctrl.Result{}, err
		}
		if counts.Failed > 0 || counts.Other > 0 && slowDispatch(aw) {
			// set failed status
			return ctrl.Result{}, r.updateStatus(ctx, aw, "Failed")
		}
		if counts.Succeeded >= int(aw.Spec.Pods) && counts.Running == 0 && counts.Other == 0 {
			// set completed status
			return ctrl.Result{}, r.updateStatus(ctx, aw, "Completed")
		}
		if !slowDispatch(aw) {
			return ctrl.Result{RequeueAfter: time.Minute}, nil // check soon
		}
		return ctrl.Result{}, nil
	}

	if aw.Status.Phase == "" {
		// add finalizer
		if controllerutil.AddFinalizer(aw, finalizer) {
			if err := r.Update(ctx, aw); err != nil {
				return ctrl.Result{}, err
			}
		}
		// set queued status only after adding finalizer
		return ctrl.Result{}, r.updateStatus(ctx, aw, "Queued")
	}

	// status == "Queued"

	// check if appwrapper fits available resources
	shouldDispatch, err := r.shouldDispatch(ctx, aw)
	if err != nil {
		return ctrl.Result{}, err
	}
	if shouldDispatch {
		// set dispatching status
		aw.Status.LastDispatchTime = metav1.Now()
		return ctrl.Result{}, r.updateStatus(ctx, aw, "Dispatching")
	}
	// if not, retry after a delay
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppWrapperReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// watch pods in addition to appwrappers
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcadv1alpha1.AppWrapper{}).Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

// Map labelled pods to appwrappers
func (r *AppWrapperReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if aw, ok := pod.ObjectMeta.Labels[label]; ok {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: aw}}}
	}
	return nil
}

// Update appwrapper status
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, aw *mcadv1alpha1.AppWrapper, phase string) error {
	log := log.FromContext(ctx)

	aw.Status.Phase = phase
	if err := r.Status().Update(ctx, aw); err != nil {
		return err
	}
	log.Info(phase)
	return nil
}

// Test if appwrapper fits available resources
func (r *AppWrapperReconciler) shouldDispatch(ctx context.Context, aw *mcadv1alpha1.AppWrapper) (bool, error) {
	// list all appwrappers
	aws := &mcadv1alpha1.AppWrapperList{}
	if err := r.List(ctx, aws); err != nil {
		return false, err
	}
	// compute available gpu capacity at desired priority level
	var gpus int32 = 40 // cluster capacity minus gpu usage by things other than appwrappers
	for _, a := range aws.Items {
		if (slices.Contains([]string{"Dispatching", "Running", "Terminating", "Failed"}, a.Status.Phase)) &&
			a.Spec.Priority >= aw.Spec.Priority {
			gpus -= a.Spec.Gpus
		}
	}
	return aw.Spec.Gpus <= gpus, nil
}

// Monitor appwrapper pods
func (r *AppWrapperReconciler) monitorPods(ctx context.Context, aw *mcadv1alpha1.AppWrapper) (*PodCounts, error) {
	// list matching pods
	pods := &v1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(aw.ObjectMeta.Namespace), client.MatchingLabels{label: aw.ObjectMeta.Name}); err != nil {
		return nil, err
	}
	counts := &PodCounts{}
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case "Succeeded":
			counts.Succeeded += 1
		case "Running":
			counts.Running += 1
		case "Failed":
			counts.Failed += 1
		default:
			counts.Other += 1
		}
	}
	return counts, nil
}

// Parse raw resource into client object
func (r *AppWrapperReconciler) parseResource(aw *mcadv1alpha1.AppWrapper, raw []byte) (client.Object, error) {
	into, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}
	obj := into.(client.Object)
	namespaced, err := r.IsObjectNamespaced(obj)
	if err != nil {
		return nil, err
	}
	if namespaced && obj.GetNamespace() == "" {
		obj.SetNamespace(aw.ObjectMeta.Namespace) // use appwrapper namespace as default
	}
	obj.SetLabels(map[string]string{label: aw.ObjectMeta.Name}) // add appwrapper label
	return obj, nil
}

// Create wrapped resources
func (r *AppWrapperReconciler) createResources(ctx context.Context, aw *mcadv1alpha1.AppWrapper) error {
	for _, resource := range aw.Spec.Resources {
		obj, err := r.parseResource(aw, resource.Raw)
		if err != nil {
			return err
		}
		if err := r.Create(ctx, obj); err != nil {
			if !errors.IsAlreadyExists(err) { // ignore existing resources
				return err
			}
		}
	}
	return nil
}

// Delete wrapped resources
func (r *AppWrapperReconciler) deleteResources(ctx context.Context, aw *mcadv1alpha1.AppWrapper) error {
	for _, resource := range aw.Spec.Resources {
		obj, err := r.parseResource(aw, resource.Raw)
		if err != nil {
			return err
		}
		if err := r.Delete(ctx, obj); err != nil {
			if !errors.IsNotFound(err) { // ignore missing resources
				return err
			}
		}
	}
	return nil
}

// Count wrapped resources
func (r *AppWrapperReconciler) countResources(ctx context.Context, aw *mcadv1alpha1.AppWrapper) (int, error) {
	count := 0
	for _, resource := range aw.Spec.Resources {
		obj, err := r.parseResource(aw, resource.Raw)
		if err != nil {
			return count, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if !errors.IsNotFound(err) { // ignore missing resources
				return count, err
			}
		} else {
			count += 1
		}
	}
	return count, nil
}

func slowDispatch(aw *mcadv1alpha1.AppWrapper) bool {
	return metav1.Now().After(aw.Status.LastDispatchTime.Add(2 * time.Minute))
}

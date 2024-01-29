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

package kueue

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	workloadv1alpha1 "github.com/project-codeflare/mcad/api/v1alpha1"
)

const (
	boxedJobLabel     = "workload.codeflare.dev/boxedjob"
	boxedJobFinalizer = "workload.codeflare.dev/finalizer"
)

// BoxedJobReconciler reconciles a BoxedJob object
type BoxedJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=boxedjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=boxedjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=boxedjobs/finalizers,verbs=update

// Reconcile reconciles a BoxedJob object
func (r *BoxedJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	job := &workloadv1alpha1.BoxedJob{}
	if err := r.Get(ctx, req.NamespacedName, job); err != nil {
		return ctrl.Result{}, nil
	}

	// handle deletion first
	if !job.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(job, boxedJobFinalizer) {
			if !r.deleteComponents(ctx, job) {
				// one or more components are still terminating
				if job.Status.Phase != workloadv1alpha1.BoxedJobDeleting {
					return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobDeleting) // update status
				}
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil // check after a short while
			}
			if controllerutil.RemoveFinalizer(job, boxedJobFinalizer) {
				if err := r.Update(ctx, job); err != nil {
					return ctrl.Result{}, err
				}
				log.FromContext(ctx).Info("Deleted")
			}
		}
		return ctrl.Result{}, nil
	}

	switch job.Status.Phase {
	case workloadv1alpha1.BoxedJobEmpty: // initial state, inject finalizer
		if controllerutil.AddFinalizer(job, boxedJobFinalizer) {
			if err := r.Update(ctx, job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobSuspended)

	case workloadv1alpha1.BoxedJobSuspended: // no components deployed
		if job.Spec.Suspend {
			return ctrl.Result{}, nil // remain suspended
		}
		return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobDeploying) // begin deployment

	case workloadv1alpha1.BoxedJobDeploying: // deploying components
		if job.Spec.Suspend {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobSuspending) // abort deployment
		}
		err, fatal := r.createComponents(ctx, job)
		if err != nil {
			if fatal {
				return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobDeleting) // abort on fatal error
			}
			return ctrl.Result{}, err // retry creation on transient error
		}
		return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobRunning)

	case workloadv1alpha1.BoxedJobRunning: // components deployed
		if job.Spec.Suspend {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobSuspending) // begin undeployment
		}
		completed, err := r.hasCompleted(ctx, job)
		if err != nil {
			return ctrl.Result{}, err
		}
		if completed {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobCompleted)
		}
		failed, err := r.hasFailed(ctx, job)
		if err != nil {
			return ctrl.Result{}, err
		}
		if failed {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobDeleting)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil

	case workloadv1alpha1.BoxedJobSuspending: // undeploying components
		// finish undeploying components irrespective of desired state (suspend bit)
		if r.deleteComponents(ctx, job) {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobSuspended)
		} else {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

	case workloadv1alpha1.BoxedJobDeleting: // deleting components on failure
		if r.deleteComponents(ctx, job) {
			return r.updateStatus(ctx, job, workloadv1alpha1.BoxedJobFailed)
		} else {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BoxedJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&workloadv1alpha1.BoxedJob{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podMapFunc)).
		Complete(r)
}

func (r *BoxedJobReconciler) podMapFunc(ctx context.Context, obj client.Object) []reconcile.Request {
	pod := obj.(*v1.Pod)
	if name, ok := pod.Labels[boxedJobLabel]; ok {
		if pod.Status.Phase == v1.PodSucceeded {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: name}}}
		}
	}
	return nil
}

func parseComponent(job *workloadv1alpha1.BoxedJob, raw []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := unstructured.UnstructuredJSONScheme.Decode(raw, nil, obj); err != nil {
		return nil, err
	}
	// TODO: use heuristic to inject labels
	namespace := obj.GetNamespace()
	if namespace == "" {
		obj.SetNamespace(job.Namespace)
	} else if namespace != job.Namespace {
		return nil, fmt.Errorf("component namespace \"%s\" is different from job namespace \"%s\"", namespace, job.Namespace)
	}
	return obj, nil
}

func parseComponents(job *workloadv1alpha1.BoxedJob) ([]client.Object, error) {
	components := job.Spec.Components
	objects := make([]client.Object, len(components))
	for i, component := range components {
		obj, err := parseComponent(job, component.Template.Raw)
		if err != nil {
			return nil, err
		}
		objects[i] = obj
	}
	return objects, nil
}

func (r *BoxedJobReconciler) createComponents(ctx context.Context, job *workloadv1alpha1.BoxedJob) (error, bool) {
	objects, err := parseComponents(job)
	if err != nil {
		return err, true // fatal
	}
	for _, obj := range objects {
		if err := r.Create(ctx, obj); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue // ignore existing component
			}
			return err, meta.IsNoMatchError(err) || apierrors.IsInvalid(err) // fatal
		}
	}
	return nil, false
}

func (r *BoxedJobReconciler) deleteComponents(ctx context.Context, job *workloadv1alpha1.BoxedJob) bool {
	// TODO forceful deletion
	log := log.FromContext(ctx)
	remaining := 0
	for _, component := range job.Spec.Components {
		obj, err := parseComponent(job, component.Template.Raw)
		if err != nil {
			log.Error(err, "Parsing error")
			continue
		}
		if err := r.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "Deletion error")
			}
			continue
		}
		remaining++ // no error deleting resource, resource therefore still exists
	}
	return remaining == 0
}

func (r *BoxedJobReconciler) updateStatus(ctx context.Context, job *workloadv1alpha1.BoxedJob, phase workloadv1alpha1.BoxedJobPhase) (ctrl.Result, error) {
	job.Status.Phase = phase
	if err := r.Status().Update(ctx, job); err != nil {
		return ctrl.Result{}, err
	}
	log.FromContext(ctx).Info(string(phase), "phase", phase)
	return ctrl.Result{}, nil
}

func (r *BoxedJobReconciler) hasCompleted(ctx context.Context, job *workloadv1alpha1.BoxedJob) (bool, error) {
	pods := &v1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{boxedJobLabel: job.Name}); err != nil {
		return false, err
	}
	var succeeded int32
	for _, pod := range pods.Items {
		switch pod.Status.Phase {
		case v1.PodSucceeded:
			succeeded += 1
		default:
			return false, nil
		}
	}
	var expected int32
	for _, c := range job.Spec.Components {
		for _, s := range c.PodSets {
			if s.Replicas == nil {
				expected++
			} else {
				expected += *s.Replicas
			}
		}
	}
	return succeeded >= expected, nil
}

func (r *BoxedJobReconciler) hasFailed(ctx context.Context, job *workloadv1alpha1.BoxedJob) (bool, error) {
	return false, nil // TODO detect failures
}

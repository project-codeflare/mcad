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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
)

// All AppWrapperReconcilers need permission to edit appwrappers

//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=workload.codeflare.dev,resources=appwrappers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch

// AppWrapperReconciler is the super type of Dispatcher and Runner reconcilers
type AppWrapperReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Cache            map[types.UID]*CachedAppWrapper // cache AppWrapper updates for write/read consistency
	MultiClusterMode bool                            // are we operating in multi-cluster mode
	ControllerName   string                          // name of the controller
}

const (
	nameLabel         = "appwrapper.mcad.ibm.com"                     // owner name label for wrapped resources
	namespaceLabel    = "appwrapper.mcad.ibm.com/namespace"           // owner namespace label for wrapped resources
	dispatchFinalizer = "workload.codeflare.dev/finalizer_dispatcher" // finalizer name for dispatcher
	runnerFinalizer   = "workload.codeflare.dev/finalizer_runner"     // finalizer name for runner
	nvidiaGpu         = "nvidia.com/gpu"                              // GPU resource name
	specNodeName      = ".spec.nodeName"                              // key to index pods based on node placement
)

// Structured logger
var mcadLog = ctrl.Log.WithName("MCAD")

func withAppWrapper(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) context.Context {
	return log.IntoContext(ctx, mcadLog.WithValues("namespace", appWrapper.Namespace, "name", appWrapper.Name, "uid", appWrapper.UID))
}

// Update AppWrapper status
// When adding a new call, please update ./docs/state-diagram.md
func (r *AppWrapperReconciler) updateStatus(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper, state mcadv1beta1.AppWrapperState, step mcadv1beta1.AppWrapperStep, reason ...string) (ctrl.Result, error) {
	// log transition
	now := metav1.Now()
	transition := mcadv1beta1.AppWrapperTransition{Time: now, Controller: r.ControllerName, State: state, Step: step}
	if len(reason) > 0 {
		transition.Reason = reason[0]
	}
	appWrapper.Status.Transitions = append(appWrapper.Status.Transitions, transition)
	if len(appWrapper.Status.Transitions) > 20 {
		appWrapper.Status.Transitions = appWrapper.Status.Transitions[1:]
	}
	appWrapper.Status.TransitionCount++
	appWrapper.Status.State = state
	appWrapper.Status.Step = step
	// update AppWrapper status in etcd, requeue reconciliation on failure
	if err := r.Status().Update(ctx, appWrapper); err != nil {
		return ctrl.Result{}, err
	}
	// cache AppWrapper status
	r.addCachedAW(appWrapper)
	log.FromContext(ctx).Info(string(state), "state", state, "step", step)
	return ctrl.Result{}, nil
}

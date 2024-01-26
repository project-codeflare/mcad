/*
Copyright 2023 The Kubernetes Authors.

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"

	workloadv1alpha1 "github.com/project-codeflare/mcad/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/finalizers,verbs=update
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=resourceflavors,verbs=get;list;watch
// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloadpriorityclasses,verbs=get;list;watch

type BoxedJob workloadv1alpha1.BoxedJob

var (
	GVK           = workloadv1alpha1.GroupVersion.WithKind("BoxedJob")
	NewReconciler = jobframework.NewGenericReconciler(func() jobframework.GenericJob { return &BoxedJob{} }, nil)
)

func (j *BoxedJob) Object() client.Object {
	return (*workloadv1alpha1.BoxedJob)(j)
}

func (j *BoxedJob) IsSuspended() bool {
	return j.Spec.Suspend
}

func (j *BoxedJob) IsActive() bool {
	return j.Status.Phase == workloadv1alpha1.BoxedJobDeploying ||
		j.Status.Phase == workloadv1alpha1.BoxedJobRunning ||
		j.Status.Phase == workloadv1alpha1.BoxedJobSuspending ||
		j.Status.Phase == workloadv1alpha1.BoxedJobDeleting
}

func (j *BoxedJob) Suspend() {
	j.Spec.Suspend = true
}

func (j *BoxedJob) GVK() schema.GroupVersionKind {
	return GVK
}

func (j *BoxedJob) PodSets() []kueue.PodSet {
	podSets := make([]kueue.PodSet, len(j.Spec.Components))

	for index := range j.Spec.Components {
		component := &j.Spec.Components[index]
		replicas := int32(1)
		// TODO account for all elements in the Topology array
		if component.Topology[0].Count != nil {
			replicas = *component.Topology[0].Count
		}
		podSets[index] = kueue.PodSet{
			Name: j.Name + "-" + fmt.Sprint(index),
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Requests: component.Topology[0].Requests}}}},
			},
			Count: replicas,
		}
	}
	return podSets
}

func (j *BoxedJob) RunWithPodSetsInfo(podSetsInfo []podset.PodSetInfo) error {
	// TODO patch the boxedjob?
	j.Spec.Suspend = false
	return nil
}

func (j *BoxedJob) RestorePodSetsInfo(podSetsInfo []podset.PodSetInfo) bool {
	// TODO unpatch the boxedjob?
	return false
}

func (j *BoxedJob) Finished() (metav1.Condition, bool) {
	condition := metav1.Condition{
		Type:   kueue.WorkloadFinished,
		Status: metav1.ConditionTrue,
		Reason: "BoxedJobFinished",
	}
	var finished bool
	switch j.Status.Phase {
	case workloadv1alpha1.BoxedJobCompleted:
		finished = true
		condition.Message = "BoxedJob finished successfully"
	case workloadv1alpha1.BoxedJobFailed:
		finished = true
		condition.Message = "BoxedJob failed"
	}
	return condition, finished
}

func (j *BoxedJob) PodsReady() bool {
	return true // TODO
}

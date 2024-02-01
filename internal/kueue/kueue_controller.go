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

package kueue

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"

	workloadv1alpha1 "github.com/project-codeflare/mcad/api/v1alpha1"
)

// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=list;get;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
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
	podSets := []kueue.PodSet{}
	i := 0
	for _, component := range j.Spec.Components {
	LOOP:
		for _, podSet := range component.PodSets {
			replicas := int32(1)
			if podSet.Replicas != nil {
				replicas = *podSet.Replicas
			}
			obj := &unstructured.Unstructured{}
			if _, _, err := unstructured.UnstructuredJSONScheme.Decode(component.Template.Raw, nil, obj); err != nil {
				continue LOOP // TODO handle error
			}
			parts := strings.Split(podSet.Path, ".")
			p := obj.UnstructuredContent()
			var ok bool
			for i := 1; i < len(parts); i++ {
				p, ok = p[parts[i]].(map[string]interface{})
				if !ok {
					continue LOOP // TODO handle error
				}
			}
			var template v1.PodTemplateSpec
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(p, &template); err != nil {
				continue LOOP // TODO handle error
			}
			podSets = append(podSets, kueue.PodSet{
				Name:     j.Name + "-" + fmt.Sprint(i),
				Template: template,
				Count:    replicas,
			})
			i++
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

/*
Copyright 2024 IBM Corporation.

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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/kueue/pkg/controller/jobframework"

	workloadv1alpha1 "github.com/project-codeflare/mcad/api/v1alpha1"
)

type BoxedJobWebhook struct {
	ManageJobsWithoutQueueName bool
}

//+kubebuilder:webhook:path=/mutate-workload-codeflare-dev-v1alpha1-boxedjob,mutating=true,failurePolicy=fail,sideEffects=None,groups=workload.codeflare.dev,resources=boxedjobs,verbs=create;update,versions=v1alpha1,name=mboxedjob.kb.io,admissionReviewVersions=v1

var _ webhook.CustomDefaulter = &BoxedJobWebhook{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (w *BoxedJobWebhook) Default(ctx context.Context, obj runtime.Object) error {
	job := obj.(*workloadv1alpha1.BoxedJob)
	log.FromContext(ctx).Info("Applying defaults", "job", job)
	jobframework.ApplyDefaultForSuspend((*BoxedJob)(job), w.ManageJobsWithoutQueueName)
	return nil
}

//+kubebuilder:webhook:path=/validate-workload-codeflare-dev-v1alpha1-boxedjob,mutating=false,failurePolicy=fail,sideEffects=None,groups=workload.codeflare.dev,resources=boxedjobs,verbs=create;update,versions=v1alpha1,name=vboxedjob.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &BoxedJobWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *BoxedJobWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	job := obj.(*workloadv1alpha1.BoxedJob)
	log.FromContext(ctx).Info("Validating create", "job", job)
	return nil, w.validateCreate(job).ToAggregate()
}

func (w *BoxedJobWebhook) validateCreate(job *workloadv1alpha1.BoxedJob) field.ErrorList {
	var allErrors field.ErrorList
	kueueJob := (*BoxedJob)(job)

	if w.ManageJobsWithoutQueueName || jobframework.QueueName(kueueJob) != "" {
		components := job.Spec.Components
		componentsPath := field.NewPath("spec").Child("components")
		podSpecCount := 0
		for idx, component := range components {
			podSetsPath := componentsPath.Index(idx).Child("podSets")
			for psIdx, ps := range component.PodSets {
				podSetPath := podSetsPath.Index(psIdx)
				if ps.Path == "" {
					allErrors = append(allErrors, field.Required(podSetPath.Child("path"), "podspec must specify path"))
				}

				// TODO: Validatate the ps.Path resolves to a PodSpec

				// TODO: RBAC check to make sure that the user has the ability to create the wrapped resources

				podSpecCount += 1
			}
		}
		if podSpecCount == 0 {
			allErrors = append(allErrors, field.Invalid(componentsPath, components, "components contains no podspecs"))
		}
		if podSpecCount > 8 {
			allErrors = append(allErrors, field.Invalid(componentsPath, components, fmt.Sprintf("components contains %v podspecs; at most 8 are allowed", podSpecCount)))
		}
	}

	allErrors = append(allErrors, jobframework.ValidateCreateForQueueName(kueueJob)...)
	return allErrors
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *BoxedJobWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldJob := oldObj.(*workloadv1alpha1.BoxedJob)
	newJob := newObj.(*workloadv1alpha1.BoxedJob)
	if w.ManageJobsWithoutQueueName || jobframework.QueueName((*BoxedJob)(newJob)) != "" {
		log.FromContext(ctx).Info("Validating update", "job", newJob)
		allErrors := jobframework.ValidateUpdateForQueueName((*BoxedJob)(oldJob), (*BoxedJob)(newJob))
		allErrors = append(allErrors, w.validateCreate(newJob)...)
		allErrors = append(allErrors, jobframework.ValidateUpdateForWorkloadPriorityClassName((*BoxedJob)(oldJob), (*BoxedJob)(newJob))...)
		return nil, allErrors.ToAggregate()
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (w *BoxedJobWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

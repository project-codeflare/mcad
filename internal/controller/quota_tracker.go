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
	"strings"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
	v1 "k8s.io/api/core/v1"
)

// Should be defined in api/core/v1/types.go
const DefaultResourceLimitsPrefix = "limits."

// A tracker of allocated quota, mapped by namespace
type QuotaTracker struct {
	state                map[string]*QuotaState
	unAdmittedWeightsMap map[string]*WeightsPair
}

// Create a new QuotaTracker
func NewQuotaTracker() *QuotaTracker {
	return &QuotaTracker{
		state:                map[string]*QuotaState{},
		unAdmittedWeightsMap: map[string]*WeightsPair{},
	}
}

// State includes total quota, used quota, and currently allocated quota
type QuotaState struct {
	quota     *WeightsPair
	used      *WeightsPair
	allocated *WeightsPair
}

// Create a QuotaState from a ResourceQuota object
func NewQuotaStateFromResourceQuota(resourceQuota *v1.ResourceQuota) *QuotaState {
	quotaWeights, usedWeights := getQuotaAndUsedWeightsPairsForResourceQuota(resourceQuota)
	return &QuotaState{
		quota:     quotaWeights,
		used:      usedWeights,
		allocated: NewWeightsPair(Weights{}, Weights{}),
	}
}

// Account for all in-flight AppWrappers with their resource demand not yet reflected in
// the Used status of any ResourceQuota object in their corresponding namespace
func (tracker *QuotaTracker) Init(weightsPairMap map[string]*WeightsPair) {
	tracker.unAdmittedWeightsMap = weightsPairMap
}

// Check if (used + allocated) <= quota
func (qs *QuotaState) IsValid() bool {
	available := qs.quota.Clone()
	available.Sub(qs.used)
	fits, _ := qs.allocated.Fits(available)
	return fits
}

// Check if the resource demand of an AppWrapper satisfies a ResourceQuota,
// without changing the current quota allocation
func (tracker *QuotaTracker) Satisfies(appWrapperAskWeights *WeightsPair, resourceQuota *v1.ResourceQuota) bool {
	namespace := resourceQuota.GetNamespace()
	var quotaState *QuotaState
	var exists bool
	if quotaState, exists = tracker.state[namespace]; !exists {
		quotaState = NewQuotaStateFromResourceQuota(resourceQuota)
		tracker.state[namespace] = quotaState
	}
	// check if both appwrapper requests and limits fit available resource quota
	quotaWeights := quotaState.quota.Clone()
	quotaWeights.Sub(quotaState.used)
	quotaWeights.Sub(quotaState.allocated)
	if unAdmittedWeights, exists := tracker.unAdmittedWeightsMap[namespace]; exists {
		quotaWeights.Sub(unAdmittedWeights)
	}
	quotaFits, _ := appWrapperAskWeights.Fits(quotaWeights)

	mcadLog.Info("QuotaTracker.Satisfies():", "namespace", namespace,
		"QuotaWeights", quotaState.quota, "UsedWeights", quotaState.used,
		"AllocatedWeights", quotaState.allocated, "AvailableWeights", quotaWeights,
		"appWrapperAskWeights", appWrapperAskWeights, "quotaFits", quotaFits)

	return quotaFits
}

// Update the QuotaState by the allocated weights of an AppWrapper in a namespace,
// fails if QuotaState does not exist in the QuotaTracker
func (tracker *QuotaTracker) Allocate(namespace string, appWrapperAskWeights *WeightsPair) bool {
	if state, exists := tracker.state[namespace]; exists && appWrapperAskWeights != nil {
		state.allocated.Add(appWrapperAskWeights)
		return true
	}
	return false
}

// Get requests and limits from AppWrapper specs
func getWeightsPairForAppWrapper(appWrapper *mcadv1beta1.AppWrapper) *WeightsPair {
	requests := aggregateRequests(appWrapper)
	limits := aggregateLimits(appWrapper)
	return NewWeightsPair(requests, limits)
}

// Get requests and limits for both quota and used from ResourceQuota status
func getQuotaAndUsedWeightsPairsForResourceQuota(resourceQuota *v1.ResourceQuota) (quotaWeights *WeightsPair,
	usedWeights *WeightsPair) {
	quotaWeights = getWeightsPairForResourceList(&resourceQuota.Status.Hard)
	usedWeights = getWeightsPairForResourceList(&resourceQuota.Status.Used)
	return quotaWeights, usedWeights
}

// Create a pair of Weights for requests and limits
// given in a ResourceList of a ResourceQuota
func getWeightsPairForResourceList(r *v1.ResourceList) *WeightsPair {
	requests := Weights{}
	limits := Weights{}
	for k, v := range *r {
		if strings.HasPrefix(k.String(), DefaultResourceLimitsPrefix) {
			trimmedName := strings.Replace(k.String(), DefaultResourceLimitsPrefix, "", 1)
			if value, exists := limits[v1.ResourceName(trimmedName)]; !exists || value.Cmp(v.AsDec()) < 0 {
				limits[v1.ResourceName(trimmedName)] = v.AsDec()
			}
			continue
		}
		if strings.HasPrefix(k.String(), v1.DefaultResourceRequestsPrefix) {
			trimmedName := strings.Replace(k.String(), v1.DefaultResourceRequestsPrefix, "", 1)
			k = v1.ResourceName(trimmedName)
		}
		if value, exists := requests[k]; !exists || value.Cmp(v.AsDec()) < 0 {
			requests[k] = v.AsDec()
		}
	}
	return NewWeightsPair(requests, limits)
}

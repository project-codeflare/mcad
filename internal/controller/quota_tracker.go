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

const DefaultResourceLimitsPrefix = "limits."

// QuotaTracker : A tracker of allocated quota, mapped by namespace
type QuotaTracker struct {
	state map[string]*QuotaState
}

// NewQuotaTracker : Create a new QuotaTracker
func NewQuotaTracker() *QuotaTracker {
	return &QuotaTracker{
		state: map[string]*QuotaState{},
	}
}

// QuotaState : State includes total quota, used quota, and currently allocated quota
type QuotaState struct {
	quota     *WeightsPair
	used      *WeightsPair
	allocated *WeightsPair
}

// NewQuotaStateFromResourceQuota : Create a QuotaState from a ResourceQuota object
func NewQuotaStateFromResourceQuota(resourceQuota *v1.ResourceQuota) *QuotaState {
	quotaWeights, usedWeights := getWeightsPairForResourceQuota(resourceQuota)
	return &QuotaState{
		quota:     quotaWeights,
		used:      usedWeights,
		allocated: NewWeightsPair(&Weights{}, &Weights{}),
	}
}

// Satisfies : Check if the allocation of an AppWrapper satisfies a ResourceQuota for a namespace,
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
	quotaFits := appWrapperAskWeights.Fits(quotaWeights)

	mcadLog.Info("QuotaTracker.Satisfies():", "namespace", namespace,
		"QuotaWeights", quotaState.quota, "UsedWeights", quotaState.used,
		"AllocatedWeights", quotaState.allocated, "AvailableWeights", quotaWeights,
		"appWrapperAskWeights", appWrapperAskWeights, "quotaFits", quotaFits)

	return quotaFits
}

// UpdateState : Update the QuotaState by an allocated weights of an AppWrapper in a namespace,
// fails if QuotaState does not exist in the QuotaTracker
func (tracker *QuotaTracker) UpdateState(namespace string, appWrapperAskWeights *WeightsPair) bool {
	if state, exists := tracker.state[namespace]; exists && appWrapperAskWeights != nil {
		state.allocated.Add(appWrapperAskWeights)
		return true
	}
	return false
}

// getWeightsPairForAppWrapper : Get requests and limits from AppWrapper specs
func getWeightsPairForAppWrapper(appWrapper *mcadv1beta1.AppWrapper) *WeightsPair {
	requests := aggregateRequests(appWrapper)
	limits := aggregateLimits(appWrapper)
	return NewWeightsPair(&requests, &limits)
}

// getWeightsPairForResourceQuota : Get requests and limits for both quota and used from ResourceQuota specs
func getWeightsPairForResourceQuota(resourceQuota *v1.ResourceQuota) (quotaWeights *WeightsPair,
	usedWeights *WeightsPair) {
	quotaWeights = getResourceRequestsLimitsWeights(&resourceQuota.Status.Hard)
	usedWeights = getResourceRequestsLimitsWeights(&resourceQuota.Status.Used)
	return quotaWeights, usedWeights
}

// getResourceRequestsLimitsWeights : Create a pair of Weights for requests and limits
// in a ResourceList of a ResourceQuota
func getResourceRequestsLimitsWeights(r *v1.ResourceList) *WeightsPair {
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
	return NewWeightsPair(&requests, &limits)
}

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
	"errors"
	"time"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// We cache AppWrappers because the reconciler cache does not immediately reflect updates.
// A Get or List call soon after an Update or Status.Update call may not reflect the latest object.
// See https://github.com/kubernetes-sigs/controller-runtime/issues/1622.
// We use the number of transitions to confirm our cached version is more recent than the reconciler cache.
// When reconciling an AppWrapper, we proactively detect and abort on conflicts.
// To defend against bugs in the cache implementation and egregious AppWrapper edits,
// we eventually give up on persistent conflicts and remove the AppWrapper from the cache.

// TODO garbage collection

// Cached AppWrapper
type CachedAppWrapper struct {
	// AppWrapper state
	State mcadv1beta1.AppWrapperState

	// AppWrapper step
	Step mcadv1beta1.AppWrapperStep

	// Number of transitions
	TransitionCount int32

	// First conflict detected between reconciler cache and our cache if not nil
	Conflict *time.Time
}

// Add AppWrapper to cache
func (r *AppWrapperReconciler) addCachedAW(appWrapper *mcadv1beta1.AppWrapper) {
	r.Cache[appWrapper.UID] = &CachedAppWrapper{State: appWrapper.Status.State, Step: appWrapper.Status.Step, TransitionCount: appWrapper.Status.TransitionCount}
}

// Remove AppWrapper from cache
func (r *AppWrapperReconciler) deleteCachedAW(appWrapper *mcadv1beta1.AppWrapper) {
	delete(r.Cache, appWrapper.UID)
}

// Get AppWrapper from cache if available or from AppWrapper if not
func (r *AppWrapperReconciler) getCachedAW(appWrapper *mcadv1beta1.AppWrapper) (mcadv1beta1.AppWrapperState, mcadv1beta1.AppWrapperStep) {
	if cached, ok := r.Cache[appWrapper.UID]; ok && cached.TransitionCount > appWrapper.Status.TransitionCount {
		return cached.State, cached.Step // our cache is more up-to-date than the reconciler cache
	}
	return appWrapper.Status.State, appWrapper.Status.Step
}

// Check whether reconciler cache and our cache appear to be in sync
func (r *AppWrapperReconciler) isStale(ctx context.Context, appWrapper *mcadv1beta1.AppWrapper) bool {
	if cached, ok := r.Cache[appWrapper.UID]; ok {
		status := appWrapper.Status
		// check number of transitions
		if cached.TransitionCount < status.TransitionCount {
			// our cache is behind, update our cache, this is ok
			r.Cache[appWrapper.UID] = &CachedAppWrapper{State: status.State, TransitionCount: status.TransitionCount}
			cached.Conflict = nil // clear conflict timestamp
			return false
		}
		if cached.TransitionCount > status.TransitionCount {
			// reconciler cache appears to be behind
			if cached.Conflict != nil {
				if time.Now().After(cached.Conflict.Add(cacheConflictTimeout)) {
					// this has been going on for a while, assume something is wrong with our cache
					delete(r.Cache, appWrapper.UID)
					log.FromContext(ctx).Error(errors.New("cache timeout"), "Internal error")
					return true
				}
			} else {
				now := time.Now()
				cached.Conflict = &now // remember when conflict started
			}
			return true
		}
		if cached.State != status.State || cached.Step != status.Step {
			// assume something is wrong with our cache
			delete(r.Cache, appWrapper.UID)
			log.FromContext(ctx).Error(errors.New("cache conflict"), "Internal error")
			return true
		}
		// caches appear to be in sync
		cached.Conflict = nil // clear conflict timestamp
	}
	return false
}

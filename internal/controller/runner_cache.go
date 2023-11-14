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
	"errors"
	"time"

	mcadv1beta1 "github.com/tayebehbahreini/mcad/api/v1beta1"
)

// Cache AppWrapper.Status

// Add AppWrapper to cache
func (r *Runner) addCachedPhase(appWrapper *mcadv1beta1.AppWrapper) {
	r.Cache[appWrapper.UID] = &CachedAppWrapper{Phase: appWrapper.Status.RunnerStatus.Phase, Transitions: len(appWrapper.Status.RunnerStatus.Transitions)}
}

// Check whether reconciler cache and our cache appear to be in sync
func (r *Runner) checkCachedPhase(appWrapper *mcadv1beta1.AppWrapper) error {
	if cached, ok := r.Cache[appWrapper.UID]; ok {
		status := appWrapper.Status.RunnerStatus
		// check number of transitions
		if cached.Transitions < len(status.Transitions) {
			// our cache is behind, update the cache, this is ok
			r.Cache[appWrapper.UID] = &CachedAppWrapper{Phase: status.Phase, Transitions: len(status.Transitions)}
			cached.Conflict = nil // clear conflict timestamp
			return nil
		}
		if cached.Transitions > len(status.Transitions) {
			// reconciler cache appears to be behind
			if cached.Conflict != nil {
				if time.Now().After(cached.Conflict.Add(cacheConflictTimeout)) {
					// this has been going on for a while, assume something is wrong with our cache
					delete(r.Cache, appWrapper.UID)
					return errors.New("persistent cache conflict") // force redo
				}
			} else {
				now := time.Now()
				cached.Conflict = &now // remember when conflict started
			}
			return errors.New("stale reconciler cache") // force redo
		}
		if cached.Phase != status.Phase {
			// something is wrong with our cache, this should not be happening
			delete(r.Cache, appWrapper.UID)
			return errors.New("divergent phase cache") // force redo
		}
		// caches appear to be in sync
		cached.Conflict = nil // clear conflict timestamp
	}
	return nil
}

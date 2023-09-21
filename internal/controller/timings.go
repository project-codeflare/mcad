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
	"time"
)

// This file defines all the time constants used in mcad

const (
	// Timeouts
	requeuingTimeout     = 2 * time.Minute // minimum wait before aborting Requeuing
	dispatchingTimeout   = 2 * time.Minute // minimum wait before aborting Dispatching
	runningTimeout       = 5 * time.Minute // minimum wait before aborting Running
	cacheConflictTimeout = 5 * time.Minute // minimum wait before invalidating the cache
	clusterInfoTimeout   = time.Minute     // how long to cache cluster capacity

	// Cluster capacity is only refreshed when trying to dispatch AppWrappers and only after
	// the previous measurement has timed out, so it is necessary to call dispatchNext on
	// a regular basis (we do) to ensure we detect new capacity (such as new schedulable nodes)

	// RequeueAfter delays
	runDelay      = time.Minute // maximum delay before next reconciliation of a Running AppWrapper
	dispatchDelay = time.Minute // maximum delay before next "*/*" reconciliation (dispatchNext)

	// The RequeueAfter delay is the maximum delay before the next reconciliation event.
	// Reconciliation may be triggered earlier due for instance to pod phase changes.
	// Reconciliation may be delayed due to the on-going reconciliation of other events.
)

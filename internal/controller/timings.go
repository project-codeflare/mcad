/*
Copyright IBM Corporation 2023.

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
	// Cluster capacity is only refreshed when trying to dispatch AppWrappers and only after
	// the previous measurement has timed out, so it is necessary to trigger dispatch on
	// a regular basis (we do) to ensure we detect new capacity (such as new schedulable nodes)
	clusterCapacityTimeout = time.Minute // how long to cache cluster capacity

	cacheConflictTimeout = 5 * time.Minute // when to give up on a cache conflict

	// Timeouts for the creation and deletion of wrapped resources and pods
	creationTimeout = 5 * time.Minute // minimum wait before aborting an incomplete resource/pod creation
	deletionTimeout = 5 * time.Minute // minimum wait before aborting an incomplete resource deletion

	// RequeueAfter delays
	// This is the maximum delay before the next reconciliation event but reconciliation
	// may be triggered earlier due for instance to pod phase changes. Moreover, the reconciliation
	// itself may be delayed due to the on-going reconciliation of other events.
	runDelay      = time.Minute // maximum delay before next reconciliation when running pods
	dispatchDelay = time.Minute // maximum delay before triggering dispatchNext with queued AppWrappers
)

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

// This file defines all the time constants used in MCAD

const (
	// Timeouts
	cacheConflictTimeout = 5 * time.Minute // minimum wait before invalidating the cache
	clusterInfoTimeout   = time.Minute     // how often to refresh cluster capacity

	// RequeueAfter delays
	runDelay      = time.Minute     // how often to force check running AppWrapper health
	dispatchDelay = time.Minute     // how often to force dispatch
	deletionDelay = 5 * time.Second // how often to check deleted resources
)

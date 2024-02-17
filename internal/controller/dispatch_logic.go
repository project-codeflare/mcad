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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/inf.v0"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
)

type QueuingDecision struct {
	reason  mcadv1beta1.AppWrapperQueuedReason
	message string
}

// Dec2float64 converts inf.Dec to float64
func Dec2float64(d *inf.Dec) (float64, error) {
	// In general, it's not recommended to convert Dec to float64. However, we have no choice since Prometheus works with float64.
	// https://github.com/go-inf/inf/issues/7#issuecomment-504729949
	return strconv.ParseFloat(d.String(), 64)
}

// Reset requested metrics
func resetRequestedMetrics() {
	requestedCpu.Reset()
	requestedMemory.Reset()
	requestedGpu.Reset()
}

// Update requested metrics
func updateRequestedMetrics(request Weights, priority int) {
	updateRequestedMetricGeneric(request, priority, "cpu", *requestedCpu)
	updateRequestedMetricGeneric(request, priority, "memory", *requestedMemory)
	updateRequestedMetricGeneric(request, priority, "nvidia.com/gpu", *requestedGpu)
}

// Update requested metric
func updateRequestedMetricGeneric(request Weights, priority int, resourceName v1.ResourceName, metric prometheus.GaugeVec) {
	if val, exists := request[resourceName]; exists {
		resourceValue, err := Dec2float64(val)
		if err != nil {
			mcadLog.Error(err, "Unable to get requested resource", "priority", priority, "resource name", resourceName)
		} else {
			metric.WithLabelValues(strconv.Itoa(priority)).Set(resourceValue)
		}
	}
}

func assignedToCluster(aw mcadv1beta1.AppWrapper, cluster string) bool {
	return aw.Labels != nil && aw.Labels[assignedClusterLabel] == cluster
}

// buildQueue returns a dispatch ordered queue of pending AppWrappers and the resources reserved by AppWrappers at every priority level.
// AppWrappers in the returned queue must be cloned if mutated
func (r *Dispatcher) buildQueue(ctx context.Context, appWrappers *mcadv1beta1.AppWrapperList, cluster string) (map[int]Weights, []*mcadv1beta1.AppWrapper, error) {
	reserved := map[int]Weights{}        // total request per priority level
	queue := []*mcadv1beta1.AppWrapper{} // queued appWrappers

	appWrapperCount := map[stateStepPriority]int{}

	for _, appWrapper := range appWrappers.Items {
		if r.MultiClusterMode && !assignedToCluster(appWrapper, cluster) {
			continue
		}

		// get AppWrapper from cache if available as reconciler cache may be lagging
		state, step := r.getCachedAW(&appWrapper)
		priority := int(appWrapper.Spec.Priority)
		key := stateStepPriority{state, step, priority}
		if _, exists := appWrapperCount[key]; !exists {
			appWrapperCount[key] = 0
		}
		appWrapperCount[key]++
		// make sure to initialize weights for every known priority level
		if reserved[priority] == nil {
			reserved[priority] = Weights{}
		}
		if step != mcadv1beta1.Idle {
			// use max request among AppWrapper request and total request of non-terminated AppWrapper pods
			awRequest := aggregateRequests(&appWrapper)
			podRequest := Weights{}
			pods := &v1.PodList{}
			if err := r.List(ctx, pods, client.UnsafeDisableDeepCopy,
				client.MatchingLabels{namespaceLabel: appWrapper.Namespace, nameLabel: appWrapper.Name}); err != nil {
				return nil, nil, err
			}
			for _, pod := range pods.Items {
				if pod.Spec.NodeName != "" && pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
					podRequest.Add(NewWeightsForPod(&pod))
				}
			}
			// compute max
			awRequest.Max(podRequest)
			reserved[int(appWrapper.Spec.Priority)].Add(awRequest)
		} else if state == mcadv1beta1.Queued &&
			time.Now().After(appWrapper.Status.RequeueTimestamp.Add(time.Duration(appWrapper.Spec.Scheduling.Requeuing.PauseTimeInSeconds)*time.Second)) {
			// add AppWrapper to queue of candidates to dispatch
			copy := appWrapper // must copy appWrapper before taking a reference, shallow copy ok
			queue = append(queue, &copy)
		}
	}
	// update AppWrapper count metrics
	appWrappersCount.Reset()
	for key, count := range appWrapperCount {
		appWrappersCount.With(
			prometheus.Labels{
				"state":    string(key.state),
				"step":     string(key.step),
				"priority": strconv.Itoa(key.priority)},
		).Set(float64(count))
	}
	// update requested resource metrics before assertPriorities()
	resetRequestedMetrics()
	for priority, request := range reserved {
		updateRequestedMetrics(request, priority)
	}
	// propagate reservations at all priority levels to all levels below
	assertPriorities(reserved)
	// order AppWrapper queue based on priority and precedence (creation time)
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Spec.Priority > queue[j].Spec.Priority {
			return true
		}
		if queue[i].Spec.Priority < queue[j].Spec.Priority {
			return false
		}
		if queue[i].CreationTimestamp.Before(&queue[j].CreationTimestamp) {
			return true
		}
		if queue[j].CreationTimestamp.Before(&queue[i].CreationTimestamp) {
			return false
		}
		return queue[i].UID < queue[j].UID // break ties with UID to ensure total ordering
	})
	return reserved, queue, nil
}

// Find next AppWrappers to dispatch in queue order
func (r *Dispatcher) selectForDispatch(ctx context.Context, quotatracker *QuotaTracker) ([]*mcadv1beta1.AppWrapper, error) {
	allClusters := &mcadv1beta1.ClusterInfoList{}
	if err := r.List(ctx, allClusters); err != nil {
		return nil, err
	}
	allAppWrappers := &mcadv1beta1.AppWrapperList{}
	if err := r.List(ctx, allAppWrappers, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}
	selected := []*mcadv1beta1.AppWrapper{}
	logThisDispatch := time.Now().After(r.NextLoggedDispatch)
	if logThisDispatch {
		r.NextLoggedDispatch = time.Now().Add(clusterInfoTimeout)
	}

	// For each cluster, make dispatching decisions
	for _, cluster := range allClusters.Items {
		capacity := NewWeights(cluster.Status.Capacity)
		if logThisDispatch {
			mcadLog.Info("Total capacity", "cluster", cluster.Name, "capacity", capacity)
		}
		requests, queue, err := r.buildQueue(ctx, allAppWrappers, cluster.Name)
		if err != nil {
			return nil, err
		}
		// compute available cluster capacity at each priority level
		// available cluster capacity = total capacity reported in cluster info - capacity reserved by AppWrappers
		available := map[int]Weights{}
		for priority, request := range requests {
			// copy capacity before subtracting request
			available[priority] = Weights{}
			available[priority].Add(capacity)
			available[priority].Sub(request)
			if logThisDispatch {
				mcadLog.Info("Available capacity", "cluster", cluster.Name, "priority", priority, "capacity", available[priority])
			}
		}
		if logThisDispatch {
			pretty := make([]string, len(queue))
			for i, appWrapper := range queue {
				pretty[i] = appWrapper.Namespace + "/" + appWrapper.Name + ":" + string(appWrapper.UID)
			}
			mcadLog.Info("Queue", "cluster", cluster.Name, "queue", pretty)
		}
		// compute ordered slice of AppWrappers that fit on the cluster (may be empty)
		for _, appWrapper := range queue {
			request := aggregateRequests(appWrapper)
			// get resourceQuota in AppWrapper namespace, if any
			resourceQuotas := &v1.ResourceQuotaList{}
			namespace := appWrapper.GetNamespace()
			if err := r.List(ctx, resourceQuotas, client.UnsafeDisableDeepCopy,
				&client.ListOptions{Namespace: namespace}); err != nil {
				return nil, err
			}
			quotaFits := true
			var appWrapperAskWeights *WeightsPair
			insufficientResources := []v1.ResourceName{}
			if len(resourceQuotas.Items) > 0 {
				appWrapperAskWeights = getWeightsPairForAppWrapper(appWrapper)
				// assuming only one resourceQuota per nameSpace
				quotaFits, insufficientResources = quotatracker.Satisfies(appWrapperAskWeights, &resourceQuotas.Items[0])
			}
			fits, gaps := request.Fits(available[int(appWrapper.Spec.Priority)])
			if fits {
				// check if appwrapper passes resource quota (if any)
				if quotaFits {
					quotatracker.Allocate(namespace, appWrapperAskWeights)
					selected = append(selected, appWrapper.DeepCopy()) // deep copy AppWrapper
					for priority, avail := range available {
						if priority <= int(appWrapper.Spec.Priority) {
							avail.Sub(request)
						}
					}
				} else {
					var msgBuilder strings.Builder
					for _, resource := range insufficientResources {
						msgBuilder.WriteString(fmt.Sprintf("Insufficient %v. ", resource))
					}
					r.Decisions[appWrapper.UID] = &QueuingDecision{reason: mcadv1beta1.QueuedInsufficientQuota, message: msgBuilder.String()}
				}
			} else {
				var msgBuilder strings.Builder
				for _, resource := range gaps {
					msgBuilder.WriteString(
						fmt.Sprintf("Insufficient %v; requested %v but only %v available. ", resource, request[resource], available[int(appWrapper.Spec.Priority)][resource]),
					)

				}
				r.Decisions[appWrapper.UID] = &QueuingDecision{reason: mcadv1beta1.QueuedInsufficientResources, message: msgBuilder.String()}
			}
		}
	}

	return selected, nil
}

// Aggregate requests
func aggregateRequests(appWrapper *mcadv1beta1.AppWrapper) Weights {
	request := Weights{}
	for _, r := range appWrapper.Spec.Resources.GenericItems {
		for _, cpr := range r.CustomPodResources {
			request.AddProd(cpr.Replicas, NewWeights(cpr.Requests))
		}
	}
	return request
}

// Aggregate limits
func aggregateLimits(appWrapper *mcadv1beta1.AppWrapper) Weights {
	limit := Weights{}
	for _, r := range appWrapper.Spec.Resources.GenericItems {
		for _, cpr := range r.CustomPodResources {
			limit.AddProd(cpr.Replicas, NewWeights(cpr.Limits))
		}
	}
	return limit
}

// Propagate reservations at all priority levels to all levels below
func assertPriorities(w map[int]Weights) {
	keys := make([]int, len(w))
	i := 0
	for k := range w {
		keys[i] = k
		i += 1
	}
	sort.Ints(keys)
	for i := len(keys) - 1; i > 0; i-- {
		w[keys[i-1]].Add(w[keys[i]])
	}
}

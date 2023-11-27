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
	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type stateStepPriority struct {
	state    mcadv1beta1.AppWrapperPhase
	step     mcadv1beta1.AppWrapperStep
	priority int
}

var (
	appWrappersCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "appwrappers_count",
		Help:      "AppWrappers count per state, step and priority",
	}, []string{"state", "step", "priority"})
	totalCapacityCpu = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "capacity_cpu",
		Help:      "Available CPU capacity per node, excluding non-AppWrapper pods",
	}, []string{"node"})
	totalCapacityMemory = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "capacity_memory",
		Help:      "Available memory capacity per node, excluding non-AppWrapper pods",
	}, []string{"node"})
	totalCapacityGpu = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "capacity_gpu",
		Help:      "Available GPU capacity per node, excluding non-AppWrapper pods",
	}, []string{"node"})

	requestedCpu = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "requested_cpu",
		Help:      "Requested CPU per priority",
	}, []string{"priority"})
	requestedMemory = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "requested_memory",
		Help:      "Requested memory per priority",
	}, []string{"priority"})
	requestedGpu = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: "mcad",
		Name:      "requested_gpu",
		Help:      "Requested GPU per priority",
	}, []string{"priority"})
)

func init() {
	metrics.Registry.MustRegister(
		appWrappersCount,
		totalCapacityCpu,
		totalCapacityMemory,
		totalCapacityGpu,
		requestedCpu,
		requestedMemory,
		requestedGpu,
	)
}

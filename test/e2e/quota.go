//go:build private
// +build private

/*
Copyright 2019, 2021 The Multi-Cluster App Dispatcher Authors.

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

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
)

var _ = BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
})

var _ = Describe("Quota E2E Test", func() {
	var ctx context.Context
	var appwrappers []*arbv1.AppWrapper

	BeforeEach(func() {
		appwrappers = []*arbv1.AppWrapper{}
		ctx = extendContextWithClient(context.Background())
		ensureNamespaceExists(ctx)
	})

	AfterEach(func() {
		cleanupTestObjectsPtr(ctx, &appwrappers)
	})

	It("Create AppWrapper  - Generic Pod Only - Sufficient Quota 1 Tree", func() {
		aw := createGenericPodAW(ctx, "aw-generic-pod-1")
		appwrappers = append(appwrappers, aw)

		err := waitAWPodsReady(ctx, aw)
		Expect(err).NotTo(HaveOccurred())
	})

	It("Create AppWrapper  - Generic Pod Only - Insufficient Quota 1 Tree", func() {
		aw := createGenericPodAWCustomDemand(ctx, "aw-generic-large-cpu-pod-1", "9000m")
		appwrappers = append(appwrappers, aw)

		err := waitAWPodsReady(ctx, aw)
		Expect(err).To(HaveOccurred())
	})
})

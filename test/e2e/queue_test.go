/*
Copyright 2019, 2021, 2023 The Multi-Cluster App Dispatcher Authors.

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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = Describe("AppWrapper E2E Tests", func() {
	var appwrappers []*arbv1.AppWrapper

	BeforeEach(func() {
		appwrappers = []*arbv1.AppWrapper{}
	})

	AfterEach(func() {
		By("Cleaning up test objects")
		cleanupTestObjects(ctx, appwrappers)
	})

	Describe("Creation of Different GVKs", func() {
		It("StatefulSet", func() {
			aw := createStatefulSetAW(ctx, "aw-statefulset-2")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
		})

		It("Deployment", func() {
			aw := createDeploymentAW(ctx, "aw-deployment-3")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
		})

		It("Pod", func() {
			aw := createGenericPodAW(ctx, "aw-pod-1")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
		})

		It("Multiple Pods", func() {
			aw := createPodTemplateAW(ctx, "aw-podtemplate-2")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
		})

	})

	Describe("Error Handling for Invalid Resources", func() {
		It("Semantically Invalid Pod", func() {
			aw := createBadPodAW(ctx, "aw-bad-podtemplate-2")
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 10*time.Second).Should(Equal(arbv1.Failed))
		})

		It("Syntactically Invalid Pod", func() {
			aw, err := createBadGenericPodTemplateAW(ctx, "aw-generic-podtemplate-2")
			if err == nil {
				appwrappers = append(appwrappers, aw)
			}
			Expect(err).To(HaveOccurred())
		})

		It("Empty Generic Item", func() {
			aw, err := createEmptyGenericItemAW(ctx, "aw-bad-generic-item-1")
			if err == nil {
				appwrappers = append(appwrappers, aw)
			}
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Queueing and Preemption", func() {

		It("MCAD CPU Accounting Test", func() {
			By("Request 55% of cluster CPU in 2 pods")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-55-percent-cpu"), cpuDemand(0.275), 2)
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-deployment-55-percent-cpu")

			By("Request 30% of cluster CPU in 2 pods")
			aw2 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-30-percent-cpu"), cpuDemand(0.15), 2)
			appwrappers = append(appwrappers, aw2)
			Expect(waitAWPodsReady(ctx, aw2)).Should(Succeed(), "Ready pods are expected for app wrapper:aw-deployment-30-percent-cpu")
		})

		It("MCAD CPU Queueing Test", func() {
			By("Request 55% of cluster CPU in 2 pods")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-55-percent-cpu"), cpuDemand(0.275), 2)
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-deployment-55-percent-cpu")

			By("Request 50% of cluster CPU in 2 pods")
			aw2 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-50-percent-cpu"), cpuDemand(0.25), 2)
			appwrappers = append(appwrappers, aw2)
			By("Verify it was queued for insufficient resources")
			Eventually(AppWrapperQueuedReason(ctx, aw2.Namespace, aw2.Name), 30*time.Second).Should(Equal(string(arbv1.QueuedInsufficientResources)))

			By("Request 30% of cluster CPU in 2 pods")
			aw3 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-30-percent-cpu"), cpuDemand(0.15), 2)
			appwrappers = append(appwrappers, aw3)
			Expect(waitAWPodsReady(ctx, aw3)).Should(Succeed(), "Ready pods are expected for app wrapper:aw-deployment-30-percent-cpu")

			By("Free resources by deleting 55% of cluster AppWrapper")
			Expect(deleteAppWrapper(ctx, aw.Name, aw.Namespace)).Should(Succeed(), "Should have been able to delete an the initial AppWrapper")
			appwrappers = []*arbv1.AppWrapper{aw2, aw3}

			By("Wait for queued 50% AppWrapper to finally be dispatched")
			Expect(waitAWPodsReady(ctx, aw2)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-deployment-50-percent-cpu")
		})

		/*
			 * TODO: Dave DISABLED because V2 doesn't support exponential backoff of requeuing time
			It("MCAD CPU Requeuing - Completion After Enough Requeuing Times Test", func() {
				fmt.Fprintf(os.Stdout, "[e2e] Completion After Enough Requeuing Times Test - Started.\n")

				// Create a job with init containers that need 200 seconds to be ready before the container starts.
				// The requeuing mechanism is set to start at 1 minute, which is not enough time for the PODs to be completed.
				// The job should be requeued 3 times before it finishes since the wait time is doubled each time the job is requeued (i.e., initially it waits
				// for 1 minutes before requeuing, then 2 minutes, and then 4 minutes). Since the init containers take 3 minutes
				// and 20 seconds to finish, a 4 minute wait should be long enough to finish the job successfully
				aw := createJobAWWithInitContainer(ctx, "aw-job-3-init-container-1", 60, "exponential", 0)
				appwrappers = append(appwrappers, aw)

				err := waitAWPodsCompleted(ctx, aw, 8*time.Minute) // This test waits for 14 minutes to make sure all PODs complete
				Expect(err).NotTo(HaveOccurred(), "Waiting for the pods to be completed")
			})
		*/

		It("MCAD CPU Requeuing - Deletion After Maximum Requeuing Times Test", Label("slow"), func() {
			// Create a job with init containers that will never complete.
			// Configure requeuing to cycle faster than normal to reduce test time.
			rq := arbv1.RequeuingSpec{TimeInSeconds: 1, MaxNumRequeuings: 2, PauseTimeInSeconds: 1}
			aw := createJobAWWithStuckInitContainer(ctx, "aw-job-3-init-container", rq)
			appwrappers = append(appwrappers, aw)
			By("Unready pods will trigger requeuing")
			Eventually(AppWrapperQueuedReason(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(string(arbv1.QueuedRequeue)))
			By("After reaching requeuing limit job is failed")
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 6*time.Minute).Should(Equal(arbv1.Failed))
		})

		/*
			TODO: DAVE DISABLED unimplemented feature of parsing generic resources to obtain resource requests
			It("Create AppWrapper  - Generic Pod Too Big - 1 Pod", func() {
				fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper  - Generic Pod Too Big - 1 Pod - Started.\n")

				aw := createGenericPodTooBigAW(ctx, "aw-generic-big-pod-1")
				appwrappers = append(appwrappers, aw)

				err := waitAWAnyPodsExists(ctx, aw)
				Expect(err).To(HaveOccurred())
			})
		*/

		It("MCAD Custom Pod Resources Test", func() {
			// This should fit on cluster with customPodResources matching deployment resource demands so AW pods are created
			aw := createGenericDeploymentCustomPodResourcesWithCPUAW(ctx, "aw-deployment-2-550-vs-550-cpu", "550m", "550m", 2, 60)
			appwrappers = append(appwrappers, aw)
			Expect(waitAWAnyPodsExists(ctx, aw)).Should(Succeed(), "Expecting any pods for app wrapper: aw-deployment-2-550-vs-550-cpu")
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Expecting pods to be ready for app wrapper: aw-deployment-2-550-vs-550-cpu")
		})

		It("MCAD Scheduling Fail Fast Preemption Test", Label("slow"), func() {
			By("Request 55% of cluster CPU in 2 pods")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-55-percent-cpu"), cpuDemand(0.275), 2)
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-deployment-55-percent-cpu")

			By("Request 40% of cluster CPU in 1 pod")
			aw2 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-40-percent-cpu"), cpuDemand(0.4), 1)
			appwrappers = append(appwrappers, aw2)

			By("Validate that 40% AppWrapper has a pending pod")
			Expect(waitAWPodsPending(ctx, aw2)).Should(Succeed(), "Pending pods are expected for app wrapper: aw-deployment-40-percent-cpu")

			By("Request 30% of cluster CPU in two pods")
			aw3 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-30-percent-cpu"), cpuDemand(0.15), 2)
			appwrappers = append(appwrappers, aw3)

			By("Validate that 30% AppWrapper is queued for insufficient resource")
			Eventually(AppWrapperQueuedReason(ctx, aw3.Namespace, aw3.Name), 1*time.Minute).Should(Equal(string(arbv1.QueuedInsufficientResources)))

			By("Validate that 40% AppWrapper is requeued because pod never started")
			Eventually(AppWrapperQueuedReason(ctx, aw2.Namespace, aw2.Name), 2*time.Minute).Should(Equal(string(arbv1.QueuedRequeue)))

			By("Validate that the 30% AppWrapper now has ready pods")
			Expect(waitAWPodsReady(ctx, aw3)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-deployment-30-percent-cpu")
		})

		It("MCAD Scheduling Priority Preemption Test", Label("slow"), func() {
			By("Request 80% of cluster CPU in 4 pods")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-normal-priority"), cpuDemand(0.2), 4)
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-normal-priority")

			By("Request 30% of cluster CPU in one high priority pod")
			aw2 := createGenericHighPriorityDeploymentWithCPUAW(ctx, appendRandomString("aw-high-priority"), cpuDemand(0.30), 1)
			appwrappers = append(appwrappers, aw2)
			Expect(waitAWPodsReady(ctx, aw2)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-high-priority")

			By("Validate that the normal priority AppWrapper is requeued")
			Eventually(AppWrapperQueuedReason(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(string(arbv1.QueuedRequeue)))

			By("Validate that the normal priority AppWrapper's queued reason becomes insufficient resource")
			Eventually(AppWrapperQueuedReason(ctx, aw.Namespace, aw.Name), 3*time.Minute).Should(Equal(string(arbv1.QueuedInsufficientResources)))

			By("Delete high priority app wrapper")
			Expect(deleteAppWrapper(ctx, aw2.Name, aw2.Namespace)).Should(Succeed())
			appwrappers = []*arbv1.AppWrapper{aw}

			By("Validate that the normal priority AppWrapper now has ready pods")
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed(), "Ready pods are expected for app wrapper: aw-normal-priority")
		})

		/*
			  TODO: DAVE -- test depends on extracting resoure requirements from generic items
			It("MCAD Job Large Compute Requirement Test", Label("slow"), func() {
				fmt.Fprintf(os.Stdout, "[e2e] MCAD Job Large Compute Requirement Test - Started.\n")

				aw := createGenericJobAWtWithLargeCompute(ctx, "aw-test-job-with-large-comp-1")
				appwrappers = append(appwrappers, aw)
				err1 := waitAWPodsReady(ctx, aw)
				Expect(err1).NotTo(HaveOccurred())
				Eventually(AppWrapper(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(WithTransform(AppWrapperState, Equal(arbv1.Queued)))
				fmt.Fprintf(os.Stdout, "[e2e] MCAD Job Large Compute Requirement Test - Completed.\n")
			})
		*/

	})

	Describe("Detection of Completion Status", func() {
		It("MCAD Job Completion Test", func() {
			aw := createGenericJobAWWithStatus(ctx, "aw-test-job-with-comp-1")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(arbv1.Succeeded))
		})

		It("MCAD Multi-Item Job Completion Test", func() {
			aw := createGenericJobAWWithMultipleStatus(ctx, "aw-test-job-with-comp-ms-21")
			appwrappers = append(appwrappers, aw)
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(arbv1.Succeeded))
		})

		It("MCAD Item Without Status Test", func() {
			aw := createAWGenericItemWithoutStatus(ctx, "aw-test-job-with-comp-44")
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name)).Should(Equal(arbv1.Running))
			Expect(waitAWPodsReadyEx(ctx, aw, 15*time.Second, 1)).ShouldNot(Succeed(), "Expecting for pods not to be ready for app wrapper: aw-test-job-with-comp-44")
		})

		It("MCAD Job Completion No-requeue Test", Label("slow"), func() {
			aw := createGenericJobAWWithScheduleSpec(ctx, "aw-test-job-with-scheduling-spec")
			appwrappers = append(appwrappers, aw)
			By("Waiting for pods to be ready")
			Expect(waitAWPodsReady(ctx, aw)).Should(Succeed())
			By("Waiting for pods to be completed and AppWrapper to be marked Succeeded")
			Expect(waitAWPodsCompleted(ctx, aw)).Should(Succeed())
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name)).Should(Equal(arbv1.Succeeded))
		})

		/* TODO: DAVE -- Testing unimplemented state in V2.  One of the wrapped resources completes, the other runs forever.  In V1 this was encoded as RunningHoldCompletion
		It("MCAD Deployment RunningHoldCompletion Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Deployment RunningHoldCompletion Test - Started.\n")

			aw := createGenericDeploymentAWWithMultipleItems(ctx, "aw-deployment-rhc")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			Expect(err1).NotTo(HaveOccurred(), "Expecting pods to be ready for app wrapper: aw-deployment-rhc")
			Eventually(AppWrapper(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(WithTransform(AppWrapperState, Equal(arbv1.AppWrapperStateRunningHoldCompletion)))
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Deployment RuningHoldCompletion Test - Completed. Awaiting app wrapper cleanup.\n")
		})
		*/

		It("MCAD Service Created but not Succeeded/Failed", func() {
			aw := createGenericServiceAWWithNoStatus(ctx, appendRandomString("aw-service-2-status"))
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperStep(ctx, aw.Namespace, aw.Name)).Should(Equal(arbv1.Created))
			Consistently(AppWrapperState(ctx, aw.Namespace, aw.Name), 20*time.Second).ShouldNot(Or(Equal(arbv1.Succeeded), Equal(arbv1.Failed)))
		})
	})

	Describe("Load Testing", Label("slow"), func() {

		It("Create AppWrapper - Generic 50 Deployment Only - 2 pods each", func() {
			const (
				awCount    = 50
				cpuPercent = 0.005
			)

			By("Creating 50 AppWrappers")
			cpuDemand := cpuDemand(cpuPercent)
			replicas := 2
			for i := 0; i < awCount; i++ {
				name := fmt.Sprintf("aw-generic-deployment-%02d-%03d", replicas, i+1)
				aw := createGenericDeploymentWithCPUAW(ctx, name, cpuDemand, replicas)
				appwrappers = append(appwrappers, aw)
			}
			By("Waiting for 10 seconds before starting to poll")
			time.Sleep(10 * time.Second)
			uncompletedAWS := appwrappers
			// wait for pods to become ready, don't assume that they are ready in the order of submission.
			By("Polling for all pods to become ready")
			err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 3*time.Minute, false, func(ctx context.Context) (done bool, err error) {
				t := time.Now()
				toCheckAWS := make([]*arbv1.AppWrapper, 0, len(appwrappers))
				for _, aw := range uncompletedAWS {
					err := waitAWPodsReadyEx(ctx, aw, 100*time.Millisecond, int(aw.Spec.Scheduling.MinAvailable))
					if err != nil {
						toCheckAWS = append(toCheckAWS, aw)
					}
				}
				uncompletedAWS = toCheckAWS
				if len(toCheckAWS) == 0 {
					fmt.Fprintf(GinkgoWriter, "\tAll pods ready at time %s\n", t.Format(time.RFC3339))
					return true, nil
				}
				fmt.Fprintf(GinkgoWriter, "\tThere are %d app wrappers without ready pods at time %s\n", len(toCheckAWS), t.Format(time.RFC3339))
				return false, nil
			})
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "[e2e] Generic 50 Deployment Only - 2 pods each - There are %d app wrappers without ready pods, err = %v\n", len(uncompletedAWS), err)
				for _, uaw := range uncompletedAWS {
					fmt.Fprintf(GinkgoWriter, "[e2e] Generic 50 Deployment Only - 2 pods each - Uncompleted AW '%s/%s'\n", uaw.Namespace, uaw.Name)
				}
			}
			Expect(err).Should(Succeed(), "All app wrappers should have completed")
		})
	})
})

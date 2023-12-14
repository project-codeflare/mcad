//go:build !private

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
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var ctx context.Context

var _ = BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx = extendContextWithClient(context.Background())
	ensureNamespaceExists(ctx)
	updateClusterCapacity(ctx)
})

var _ = Describe("AppWrapper E2E Tests", func() {
	var appwrappers []*arbv1.AppWrapper

	BeforeEach(func() {
		appwrappers = []*arbv1.AppWrapper{}
	})

	AfterEach(func() {
		cleanupTestObjects(ctx, appwrappers)
	})

	Describe("Creation of Different GVKs", func() {
		It("Create AppWrapper - StatefulSet Only - 2 Pods", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - StatefulSet Only - 2 Pods - Started.\n")

			aw := createStatefulSetAW(ctx, "aw-statefulset-2")
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create AppWrapper - Deployment Only - 3 Pods", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - Deployment Only 3 Pods - Started.\n")

			aw := createDeploymentAW(ctx, "aw-deployment-3")
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create AppWrapper  - Pod Only - 1 Pod", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - Pod Only - 1 Pod - Started.\n")

			aw := createGenericPodAW(ctx, "aw-pod-1")
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Create AppWrapper  - PodTemplates Only - 2 Pods", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - PodTemplate Only - 2 Pods - Started.\n")

			aw := createPodTemplateAW(ctx, "aw-podtemplate-2")
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred())
		})

	})

	Describe("Error Handling for Invalid Resources", func() {
		It("Create AppWrapper- Bad Pod", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - Bad Pod - Started.\n")

			aw := createBadPodAW(ctx, "aw-bad-podtemplate-2")
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 10*time.Second).Should(Equal(arbv1.Failed))
		})

		It("Create AppWrapper  - Bad PodTemplate Only", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper - Bad PodTemplate Only - Started.\n")

			aw, err := createBadGenericPodTemplateAW(ctx, "aw-generic-podtemplate-2")
			if err == nil {
				appwrappers = append(appwrappers, aw)
			}
			Expect(err).To(HaveOccurred())
		})

		It("Create AppWrapper  - Bad Generic Item Only", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper  - Bad Generic Item Only - Started.\n")

			aw := createBadGenericItemAW(ctx, "aw-bad-generic-item-1")
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 10*time.Second).Should(Equal(arbv1.Failed))
		})
	})

	Describe("Queueing and Preemption", func() {

		It("MCAD CPU Accounting Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD CPU Accounting Test - Started.\n")

			By("Request 55% of cluster CPU")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-55-percent-cpu"), cpuDemand(0.275), 2)
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred(), "Ready pods are expected for app wrapper: aw-deployment-55-percent-cpu")

			By("Request 30% of cluster CPU")
			aw2 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-30-percent-cpu"), cpuDemand(0.15), 2)
			appwrappers = append(appwrappers, aw2)
			err = waitAWPodsReady(ctx, aw2)
			Expect(err).NotTo(HaveOccurred(), "Ready pods are expected for app wrapper:aw-deployment-30-percent-cpu")
		})

		It("MCAD CPU Queueing Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD CPU Queueing Test - Started.\n")

			By("Request 55% of cluster CPU")
			aw := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-55-percent-cpu"), cpuDemand(0.275), 2)
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred(), "Ready pods are expected for app wrapper: aw-deployment-55-percent-cpu")

			By("Request 50% of cluster CPU (will not fit; should be queued for insufficient resources)")
			aw2 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-50-percent-cpu"), cpuDemand(0.25), 2)
			appwrappers = append(appwrappers, aw2)
			Eventually(AppWrapperQueuedReason(ctx, aw2.Namespace, aw2.Name), 30*time.Second).Should(Equal(arbv1.QueuedInsufficientResources))

			By("Request 30% of cluster CPU")
			aw3 := createGenericDeploymentWithCPUAW(ctx, appendRandomString("aw-deployment-30-percent-cpu"), cpuDemand(0.15), 2)
			appwrappers = append(appwrappers, aw3)
			err = waitAWPodsReady(ctx, aw3)
			Expect(err).NotTo(HaveOccurred(), "Ready pods are expected for app wrapper:aw-deployment-30-percent-cpu")

			By("Free resources by deleting 55% of cluster AppWrapper")
			err = deleteAppWrapper(ctx, aw.Name, aw.Namespace)
			appwrappers = []*arbv1.AppWrapper{aw2, aw3}
			Expect(err).NotTo(HaveOccurred(), "Should have been able to delete an the initial AppWrapper")

			By("Wait for queued AppWrapper to finally be dispatched")
			err = waitAWPodsReady(ctx, aw2)
			Expect(err).NotTo(HaveOccurred(), "Ready pods are expected for app wrapper: aw-deployment-50-percent-cpu")
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
			fmt.Fprintf(os.Stdout, "[e2e] MCAD CPU Requeuing - Deletion After Maximum Requeuing Times Test - Started.\n")

			// Create a job with init containers that will never complete.
			// Configure requeuing to cycle faster than normal to reduce test time.
			rq := arbv1.RequeuingSpec{TimeInSeconds: 1, MaxNumRequeuings: 2, PauseTimeInSeconds: 1}
			aw := createJobAWWithStuckInitContainer(ctx, "aw-job-3-init-container", rq)
			appwrappers = append(appwrappers, aw)
			By("Unready pods will trigger requeuing")
			Eventually(AppWrapperQueuedReason(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(arbv1.QueuedRequeue))
			By("After reaching requeuing limit job is failed")
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 4*time.Minute).Should(Equal(arbv1.Failed))
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
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Custom Pod Resources Test - Started.\n")

			// This should fit on cluster with customPodResources matching deployment resource demands so AW pods are created
			aw := createGenericDeploymentCustomPodResourcesWithCPUAW(
				ctx, "aw-deployment-2-550-vs-550-cpu", "550m", "550m", 2, 60)

			appwrappers = append(appwrappers, aw)

			err := waitAWAnyPodsExists(ctx, aw)
			Expect(err).NotTo(HaveOccurred(), "Expecting any pods for app wrapper: aw-deployment-2-550-vs-550-cpu")

			err = waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred(), "Expecting pods to be ready for app wrapper: aw-deployment-2-550-vs-550-cpu")
		})

		/* TODO: DAVE STATUS + test too long + unimplemented premption features + test sensitivity to cluster size
		It("MCAD Scheduling Fail Fast Preemption Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Started.\n")

			// This should fill up the worker node and most of the master node
			aw := createDeploymentAWwith550CPU(ctx, appendRandomString("aw-deployment-2-550cpu"))
			appwrappers = append(appwrappers, aw)
			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred(), "Expecting pods for app wrapper: aw-deployment-2-550cpu")

			// This should not fit on any node but should dispatch because there is enough aggregated resources.
			aw2 := createGenericDeploymentCustomPodResourcesWithCPUAW(
				ctx, appendRandomString("aw-ff-deployment-1-850-cpu"), "850m", "850m", 1, 60)

			appwrappers = append(appwrappers, aw2)

			err = waitAWAnyPodsExists(ctx, aw2)
			Expect(err).NotTo(HaveOccurred(), "Expecting pending pods for app wrapper: aw-ff-deployment-1-850-cpu")

			err = waitAWPodsPending(ctx, aw2)
			Expect(err).NotTo(HaveOccurred(), "Expecting pending pods (try 2) for app wrapper: aw-ff-deployment-1-850-cpu")
			fmt.Fprintf(GinkgoWriter, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Pending pods found for app wrapper aw-ff-deployment-1-850-cpu\n")

			// This should fit on cluster after AW aw-deployment-1-850-cpu above is automatically preempted on
			// scheduling failure
			aw3 := createGenericDeploymentCustomPodResourcesWithCPUAW(
				ctx, appendRandomString("aw-ff-deployment-2-340-cpu"), "340m", "340m", 2, 60)

			appwrappers = append(appwrappers, aw3)

			// Wait for pods to get created, assumes preemption around 10 minutes
			err = waitAWPodsExists(ctx, aw3, 720000*time.Millisecond)
			Expect(err).NotTo(HaveOccurred(), "Expecting pods for app wrapper: aw-ff-deployment-2-340-cpu")
			fmt.Fprintf(GinkgoWriter, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Pods not found for app wrapper aw-ff-deployment-2-340-cpu\n")

			err = waitAWPodsReady(ctx, aw3)
			Expect(err).NotTo(HaveOccurred(), "Expecting no pods for app wrapper: aw-ff-deployment-2-340-cpu")
			fmt.Fprintf(GinkgoWriter, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Ready pods found for app wrapper aw-ff-deployment-2-340-cpu\n")

			// Make sure pods from AW aw-deployment-1-850-cpu have preempted
			var pass = false
			for true {
				aw2Update := &arbv1.AppWrapper{}
				err := ctx.client.Get(ctx, client.ObjectKey{Namespace: aw2.Namespace, Name: aw2.Name}, aw2Update)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Error getting AW update %v", err)
				}
				for _, cond := range aw2Update.Status.Conditions {
					if cond.Reason == "PreemptionTriggered" {
						pass = true
						fmt.Fprintf(GinkgoWriter, "[e2e] MCAD Scheduling Fail Fast Preemption Test - the pass value is %v", pass)
					}
				}
				if pass {
					break
				} else {
					time.Sleep(30 * time.Second)
				}
			}

			Expect(pass).To(BeTrue(), "Expecting AW to be preempted : aw-ff-deployment-1-850-cpu")
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Scheduling Fail Fast Preemption Test - Completed. Awaiting app wrapper cleanup\n")

		})
		*/

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

		/* TODO: DAVE -- Unimplemented Status feature: PendingPodConditions
		It("Create AppWrapper  - Check failed pod status", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Create AppWrapper  - Check failed pod status - Started.\n")

			aw := createPodCheckFailedStatusAW(ctx, "aw-checkfailedstatus-1")
			appwrappers = append(appwrappers, aw)

			err := waitAWPodsReady(ctx, aw)
			Expect(err).NotTo(HaveOccurred())
			pass := false
			for true {
				aw1 := &arbv1.AppWrapper{}
				err := ctx.client.Get(ctx, client.ObjectKey{Namespace: aw.Namespace, Name: aw.Name}, aw1)
				if err != nil {
					fmt.Fprint(GinkgoWriter, "Error getting status")
				}
				fmt.Fprintf(GinkgoWriter, "[e2e] status of AW %v.\n", aw1.Status.State)
				if len(aw1.Status.PendingPodConditions) == 0 {
					pass = true
				}
				if pass {
					break
				}
			}
			Expect(pass).To(BeTrue())
		})
		*/

		/* TODO: Dave -- testing unimplemented feature -- DispatchDuration
		It("MCAD app wrapper timeout Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD app wrapper timeout Test - Started.\n")

			aw := createGenericAWTimeoutWithStatus(ctx, "aw-test-jobtimeout-with-comp-1")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			Expect(err1).NotTo(HaveOccurred(), "Expecting pods to be ready for app wrapper: aw-test-jobtimeout-with-comp-1")
			var aw1 *arbv1.AppWrapper
			var err error
			aw1, err = ctx.karclient.WorkloadV1beta1().AppWrappers(aw.Namespace).Get(ctx, aw.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Expecting no error when getting app wrapper status")
			fmt.Fprintf(GinkgoWriter, "[e2e] status of app wrapper: %v.\n", aw1.Status)
			for aw1.Status.State != arbv1.AppWrapperStateFailed {
				aw1, err = ctx.karclient.WorkloadV1beta1().AppWrappers(aw.Namespace).Get(ctx, aw.Name, metav1.GetOptions{})
				if aw.Status.State == arbv1.AppWrapperStateFailed {
					break
				}
			}
			Expect(aw1.Status.State).To(Equal(arbv1.AppWrapperStateFailed), "Expecting a failed state")
			fmt.Fprintf(os.Stdout, "[e2e] MCAD app wrapper timeout Test - Completed.\n")
		})
		*/

		It("MCAD Job Completion Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Job Completion Test - Started.\n")

			aw := createGenericJobAWWithStatus(ctx, "aw-test-job-with-comp-1")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			Expect(err1).NotTo(HaveOccurred())
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(arbv1.Succeeded))
		})

		It("MCAD Multi-Item Job Completion Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Multi-Item Job Completion Test - Started.\n")

			aw := createGenericJobAWWithMultipleStatus(ctx, "aw-test-job-with-comp-ms-21")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			Expect(err1).NotTo(HaveOccurred(), "Expecting pods to be ready for app wrapper: 'aw-test-job-with-comp-ms-21'")
			Eventually(AppWrapperState(ctx, aw.Namespace, aw.Name), 2*time.Minute).Should(Equal(arbv1.Succeeded))
		})

		It("MCAD GenericItem Without Status Test", func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD GenericItem Without Status Test - Started.\n")

			aw := createAWGenericItemWithoutStatus(ctx, "aw-test-job-with-comp-44")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			fmt.Fprintf(GinkgoWriter, "The error is: %v", err1)
			Expect(err1).To(HaveOccurred(), "Expecting for pods not to be ready for app wrapper: aw-test-job-with-comp-44")
		})

		It("MCAD Job Completion No-requeue Test", Label("slow"), func() {
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Job Completion No-requeue Test - Started.\n")

			aw := createGenericJobAWWithScheduleSpec(ctx, "aw-test-job-with-scheduling-spec")
			appwrappers = append(appwrappers, aw)
			err1 := waitAWPodsReady(ctx, aw)
			Expect(err1).NotTo(HaveOccurred(), "Waiting for pods to be ready")
			err2 := waitAWPodsCompleted(ctx, aw)
			Expect(err2).NotTo(HaveOccurred(), "Waiting for pods to be completed")

			// Once pods are completed, we wait for them to see if they change their status to anything BUT "Completed"
			// which SHOULD NOT happen because the job is done
			err3 := waitAWPodsNotCompleted(ctx, aw)
			Expect(err3).To(HaveOccurred(), "Waiting for pods not to be completed")
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
			fmt.Fprintf(os.Stdout, "[e2e] MCAD Service Created but not Succeeded/Failed - Started.\n")

			aw := createGenericServiceAWWithNoStatus(ctx, appendRandomString("aw-service-2-status"))
			appwrappers = append(appwrappers, aw)
			Eventually(AppWrapperStep(ctx, aw.Namespace, aw.Name), 30*time.Second).Should(Equal(arbv1.Created))
			Consistently(AppWrapperState(ctx, aw.Namespace, aw.Name), 30*time.Second).ShouldNot(Or(Equal(arbv1.Succeeded), Equal(arbv1.Failed)))
		})
	})

	Describe("Load Testing", Label("slow"), func() {

		It("Create AppWrapper - Generic 50 Deployment Only - 2 pods each", func() {
			fmt.Fprintf(os.Stdout, "[e2e] Generic 50 Deployment Only - 2 pods each - Started.\n")

			const (
				awCount           = 50
				reportingInterval = 10
				cpuPercent        = 0.005
			)

			cpuDemand := cpuDemand(cpuPercent)
			replicas := 2
			modDivisor := int(awCount / reportingInterval)
			for i := 0; i < awCount; i++ {
				name := fmt.Sprintf("aw-generic-deployment-%02d-%03d", replicas, i+1)

				if ((i+1)%modDivisor) == 0 || i == 0 {
					fmt.Fprintf(GinkgoWriter, "[e2e] Creating AW %s with %s cpu and %d replica(s).\n", name, cpuDemand, replicas)
				}
				aw := createGenericDeploymentWithCPUAW(ctx, name, cpuDemand, replicas)
				appwrappers = append(appwrappers, aw)
			}
			// Give the deployments time to create pods
			time.Sleep(70 * time.Second)
			uncompletedAWS := appwrappers
			// wait for pods to become ready, don't assume that they are ready in the order of submission.
			err := wait.Poll(500*time.Millisecond, 3*time.Minute, func() (done bool, err error) {
				t := time.Now()
				toCheckAWS := make([]*arbv1.AppWrapper, 0, len(appwrappers))
				for _, aw := range uncompletedAWS {
					err := waitAWPodsReadyEx(ctx, aw, 100*time.Millisecond, int(aw.Spec.Scheduling.MinAvailable))
					if err != nil {
						toCheckAWS = append(toCheckAWS, aw)
					}
				}
				uncompletedAWS = toCheckAWS
				fmt.Fprintf(GinkgoWriter, "[e2e] Generic 50 Deployment Only - 2 pods each - There are %d app wrappers without ready pods at time %s\n", len(toCheckAWS), t.Format(time.RFC3339))
				if len(toCheckAWS) == 0 {
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "[e2e] Generic 50 Deployment Only - 2 pods each - There are %d app wrappers without ready pods, err = %v\n", len(uncompletedAWS), err)
				for _, uaw := range uncompletedAWS {
					fmt.Fprintf(GinkgoWriter, "[e2e] Generic 50 Deployment Only - 2 pods each - Uncompleted AW '%s/%s'\n", uaw.Namespace, uaw.Name)
				}
			}
			Expect(err).Should(Succeed(), "All app wrappers should have completed")
			fmt.Fprintf(os.Stdout, "[e2e] Generic 50 Deployment Only - 2 pods each - Completed, awaiting app wrapper clean up.\n")
		})
	})
})

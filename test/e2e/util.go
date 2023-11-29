/*
Copyright 2019, 2021, 2022, 2023 The Multi-Cluster App Dispatcher Authors.

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
	gcontext "context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
)

var ninetySeconds = 90 * time.Second
var threeMinutes = 180 * time.Second
var tenMinutes = 600 * time.Second
var threeHundredSeconds = 300 * time.Second

var oneCPU = v1.ResourceList{"cpu": resource.MustParse("1000m")}
var twoCPU = v1.ResourceList{"cpu": resource.MustParse("2000m")}
var threeCPU = v1.ResourceList{"cpu": resource.MustParse("3000m")}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

type context struct {
	client                 client.Client
	namespace              string
	queues                 []string
	enableNamespaceAsQueue bool
	ctx                    gcontext.Context
}

func initTestContext() *context {
	enableNamespaceAsQueue, _ := strconv.ParseBool(os.Getenv("ENABLE_NAMESPACES_AS_QUEUE"))
	cxt := &context{
		namespace: "test",
		queues:    []string{"q1", "q2"},
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = arbv1.AddToScheme(scheme)
	kubeconfig := ctrl.GetConfigOrDie()
	kubeclient, err := client.New(kubeconfig, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	cxt.client = kubeclient

	cxt.enableNamespaceAsQueue = enableNamespaceAsQueue

	_ = cxt.client.Create(gcontext.Background(), &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: cxt.namespace,
		},
	})
	// Expect(err).NotTo(HaveOccurred())

	/* 	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(gcontext.Background(), &schedv1.PriorityClass{
	   		ObjectMeta: metav1.ObjectMeta{
	   			Name: masterPriority,
	   		},
	   		Value:         100,
	   		GlobalDefault: false,
	   	}, metav1.CreateOptions{})
	   	Expect(err).NotTo(HaveOccurred())

	   	_, err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Create(gcontext.Background(), &schedv1.PriorityClass{
	   		ObjectMeta: metav1.ObjectMeta{
	   			Name: workerPriority,
	   		},
	   		Value:         1,
	   		GlobalDefault: false,
	   	}, metav1.CreateOptions{})
	   	Expect(err).NotTo(HaveOccurred()) */
	cxt.ctx = gcontext.Background()
	return cxt
}

func cleanupTestContextExtendedTime(cxt *context, seconds time.Duration) {
	// foreground := metav1.DeletePropagationForeground
	/* err := cxt.kubeclient.CoreV1().Namespaces().Delete(gcontext.Background(), cxt.namespace, metav1.DeleteOptions{
		PropagationPolicy: &foreground,
	})
	Expect(err).NotTo(HaveOccurred()) */

	// err := cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(gcontext.Background(), masterPriority, metav1.DeleteOptions{
	// 	PropagationPolicy: &foreground,
	// })
	// Expect(err).NotTo(HaveOccurred())

	// err = cxt.kubeclient.SchedulingV1beta1().PriorityClasses().Delete(gcontext.Background(), workerPriority, metav1.DeleteOptions{
	// 	PropagationPolicy: &foreground,
	// })
	// Expect(err).NotTo(HaveOccurred())

	// Wait for namespace deleted.
	// err = wait.Poll(100*time.Millisecond, seconds, namespaceNotExist(cxt))
	// if err != nil {
	// 	fmt.Fprintf(GinkgoWriter, "[cleanupTestContextExtendedTime] Failure check for namespace: %s.\n", cxt.namespace)
	// }
	// Expect(err).NotTo(HaveOccurred())
}

func cleanupTestContext(cxt *context) {
	cleanupTestContextExtendedTime(cxt, ninetySeconds)
}

type taskSpec struct {
	min, rep int32
	img      string
	hostport int32
	req      v1.ResourceList
	affinity *v1.Affinity
	labels   map[string]string
}

type jobSpec struct {
	name      string
	namespace string
	queue     string
	tasks     []taskSpec
}

func getNS(context *context, job *jobSpec) string {
	if len(job.namespace) != 0 {
		return job.namespace
	}

	if context.enableNamespaceAsQueue {
		if len(job.queue) != 0 {
			return job.queue
		}
	}

	return context.namespace
}

func createGenericAWTimeoutWithStatus(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-jobtimeout-with-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-jobtimeout-with-comp-1"
					}
				},
				"spec": {
					"containers": [
						{
							"args": [
								"sleep infinity"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-jobtimeout-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	var schedSpecMin int32 = 1
	var dispatchDurationSeconds int32 = 10
	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
				DoNotUseDispatchDuration: arbv1.DoNotUseDispatchDurationSpec{
					Limit: dispatchDurationSeconds,
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func anyPodsExist(ctx *context, awNamespace string, awName string) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := ctx.client.List(gcontext.Background(), podList, &client.ListOptions{Namespace: awNamespace})
		Expect(err).NotTo(HaveOccurred())

		podExistsNum := 0
		for _, podFromPodList := range podList.Items {

			// First find a pod from the list that is part of the AW
			if awn, found := podFromPodList.Labels["appwrapper.mcad.ibm.com"]; !found || awn != awName {
				// DEBUG fmt.Fprintf(GinkgoWriter, "[anyPodsExist] Pod %s in phase: %s not part of AppWrapper: %s, labels: %#v\n",
				// DEBUG 	podFromPodList.Name, podFromPodList.Status.Phase, awName, podFromPodList.Labels)
				continue
			}
			podExistsNum++
			fmt.Fprintf(GinkgoWriter, "[anyPodsExist] Found Pod %s in phase: %s as part of AppWrapper: %s, labels: %#v\n",
				podFromPodList.Name, podFromPodList.Status.Phase, awName, podFromPodList.Labels)
		}

		return podExistsNum > 0, nil
	}
}

func podPhase(ctx *context, awNamespace string, awName string, pods []*v1.Pod, phase []v1.PodPhase, taskNum int) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := ctx.client.List(gcontext.Background(), podList, &client.ListOptions{Namespace: awNamespace})
		Expect(err).NotTo(HaveOccurred())

		if podList == nil || podList.Size() < 1 {
			fmt.Fprintf(GinkgoWriter, "[podPhase] Listing podList found for Namespace: %s/%s resulting in no podList found that could match AppWrapper with pod count: %d\n",
				awNamespace, awName, len(pods))
		}

		phaseListTaskNum := 0

		for _, podFromPodList := range podList.Items {

			// First find a pod from the list that is part of the AW
			if awn, found := podFromPodList.Labels["appwrapper.mcad.ibm.com"]; !found || awn != awName {
				fmt.Fprintf(GinkgoWriter, "[podPhase] Pod %s in phase: %s not part of AppWrapper: %s, labels: %#v\n",
					podFromPodList.Name, podFromPodList.Status.Phase, awName, podFromPodList.Labels)
				continue
			}

			// Next check to see if it is a phase we are looking for
			for _, p := range phase {

				// If we found the phase make sure it is part of the list of pod provided in the input
				if podFromPodList.Status.Phase == p {
					matchToPodsFromInput := false
					var inputPodIDs []string
					for _, inputPod := range pods {
						inputPodIDs = append(inputPodIDs, fmt.Sprintf("%s.%s", inputPod.Namespace, inputPod.Name))
						if strings.Compare(podFromPodList.Namespace, inputPod.Namespace) == 0 &&
							strings.Compare(podFromPodList.Name, inputPod.Name) == 0 {
							phaseListTaskNum++
							matchToPodsFromInput = true
							break
						}

					}
					if !matchToPodsFromInput {
						fmt.Fprintf(GinkgoWriter, "[podPhase] Pod %s in phase: %s does not match any input pods: %#v \n",
							podFromPodList.Name, podFromPodList.Status.Phase, inputPodIDs)
					}
					break
				}
			}
		}

		return taskNum == phaseListTaskNum, nil
	}
}

func cleanupTestObjectsPtr(context *context, appwrappersPtr *[]*arbv1.AppWrapper) {
	cleanupTestObjectsPtrVerbose(context, appwrappersPtr, true)
}

func cleanupTestObjectsPtrVerbose(context *context, appwrappersPtr *[]*arbv1.AppWrapper, verbose bool) {
	if appwrappersPtr == nil {
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjectsPtr] No  AppWrappers to cleanup.\n")
	} else {
		cleanupTestObjects(context, *appwrappersPtr)
	}
}

func cleanupTestObjects(context *context, appwrappers []*arbv1.AppWrapper) {
	cleanupTestObjectsVerbose(context, appwrappers, true)
}

func cleanupTestObjectsVerbose(context *context, appwrappers []*arbv1.AppWrapper, verbose bool) {
	if appwrappers == nil {
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] No AppWrappers to cleanup.\n")
		return
	}

	for _, aw := range appwrappers {
		// context.karclient.ArbV1().AppWrappers(context.namespace).Delete(aw.Name, &metav1.DeleteOptions{PropagationPolicy: &foreground})

		pods := getPodsOfAppWrapper(context, aw)
		awNamespace := aw.Namespace
		awName := aw.Name
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Deleting AW %s.\n", aw.Name)
		err := deleteAppWrapper(context, aw.Name)
		Expect(err).NotTo(HaveOccurred())

		// Wait for the pods of the deleted the appwrapper to be destroyed
		for _, pod := range pods {
			fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Awaiting pod %s/%s to be deleted for AW %s.\n",
				pod.Namespace, pod.Name, aw.Name)
		}
		err = waitAWPodsDeleted(context, awNamespace, awName, pods)

		// Final check to see if pod exists
		if err != nil {
			var podsStillExisting []*v1.Pod
			for _, pod := range pods {
				podExist := &v1.Pod{}
				_ = context.client.Get(context.ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, podExist)
				if podExist != nil {
					fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Found pod %s/%s %s, not completedly deleted for AW %s.\n", podExist.Namespace, podExist.Name, podExist.Status.Phase, aw.Name)
					podsStillExisting = append(podsStillExisting, podExist)
				}
			}
			if len(podsStillExisting) > 0 {
				err = waitAWPodsDeleted(context, awNamespace, awName, podsStillExisting)
			}
		}
		Expect(err).NotTo(HaveOccurred())
	}
	cleanupTestContext(context)
}

func awPodPhase(ctx *context, aw *arbv1.AppWrapper, phase []v1.PodPhase, taskNum int, quite bool) wait.ConditionFunc {
	return func() (bool, error) {
		defer GinkgoRecover()
		awIgnored := &arbv1.AppWrapper{}
		err := ctx.client.Get(ctx.ctx, client.ObjectKey{Namespace: aw.Namespace, Name: aw.Name}, awIgnored) // TODO: Do we actually need to do this Get?
		Expect(err).NotTo(HaveOccurred())

		podList := &v1.PodList{}
		err = ctx.client.List(ctx.ctx, podList, &client.ListOptions{Namespace: aw.Namespace})
		Expect(err).NotTo(HaveOccurred())

		if podList == nil || podList.Size() < 1 {
			fmt.Fprintf(GinkgoWriter, "[awPodPhase] Listing podList found for Namespace: %s resulting in no podList found that could match AppWrapper: %s \n",
				aw.Namespace, aw.Name)
		}

		readyTaskNum := 0
		for _, pod := range podList.Items {
			if awn, found := pod.Labels["appwrapper.mcad.ibm.com"]; !found || awn != aw.Name {
				if !quite {
					fmt.Fprintf(GinkgoWriter, "[awPodPhase] Pod %s not part of AppWrapper: %s, labels: %s\n", pod.Name, aw.Name, pod.Labels)
				}
				continue
			}

			for _, p := range phase {
				if pod.Status.Phase == p {
					// DEBUGif quite {
					// DEBUG	fmt.Fprintf(GinkgoWriter, "[awPodPhase] Found pod %s of AppWrapper: %s, phase: %v\n", pod.Name, aw.Name, p)
					// DEBUG}
					readyTaskNum++
					break
				} else {
					pMsg := pod.Status.Message
					if len(pMsg) > 0 {
						pReason := pod.Status.Reason
						fmt.Fprintf(GinkgoWriter, "[awPodPhase] pod: %s, phase: %s, reason: %s, message: %s\n", pod.Name, p, pReason, pMsg)
					}
					containerStatuses := pod.Status.ContainerStatuses
					for _, containerStatus := range containerStatuses {
						waitingState := containerStatus.State.Waiting
						if waitingState != nil {
							wMsg := waitingState.Message
							if len(wMsg) > 0 {
								wReason := waitingState.Reason
								containerName := containerStatus.Name
								fmt.Fprintf(GinkgoWriter, "[awPodPhase] condition for pod: %s, phase: %s, container name: %s, "+
									"reason: %s, message: %s\n", pod.Name, p, containerName, wReason, wMsg)
							}
						}
					}
				}
			}
		}

		// DEBUGif taskNum <= readyTaskNum && quite {
		// DEBUG	fmt.Fprintf(GinkgoWriter, "[awPodPhase] Successfully found %v podList of AppWrapper: %s, state: %s\n", readyTaskNum, aw.Name, aw.Status.State)
		// DEBUG}

		return taskNum <= readyTaskNum, nil
	}
}

func waitAWNonComputeResourceActive(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWNamespaceActive(ctx, aw)
}

func waitAWNamespaceActive(ctx *context, aw *arbv1.AppWrapper) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, awNamespacePhase(ctx, aw,
		[]v1.NamespacePhase{v1.NamespaceActive}))
}

func awNamespacePhase(ctx *context, aw *arbv1.AppWrapper, phase []v1.NamespacePhase) wait.ConditionFunc {
	return func() (bool, error) {
		awIgnored := &arbv1.AppWrapper{}
		err := ctx.client.Get(ctx.ctx, client.ObjectKey{Namespace: aw.Namespace, Name: aw.Name}, awIgnored) // TODO: Do we actually need to do this Get?
		Expect(err).NotTo(HaveOccurred())

		namespaces := &v1.NamespaceList{}
		err = ctx.client.List(ctx.ctx, namespaces)
		Expect(err).NotTo(HaveOccurred())

		readyTaskNum := 0
		for _, namespace := range namespaces.Items {
			if awns, found := namespace.Labels["appwrapper.mcad.ibm.com"]; !found || awns != aw.Name {
				continue
			}

			for _, p := range phase {
				if namespace.Status.Phase == p {
					readyTaskNum++
					break
				}
			}
		}

		return 0 < readyTaskNum, nil
	}
}

func waitAWPodsReady(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWPodsReadyEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWPodsCompleted(ctx *context, aw *arbv1.AppWrapper, timeout time.Duration) error {
	return waitAWPodsCompletedEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false, timeout)
}

func waitAWPodsNotCompleted(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWPodsNotCompletedEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWReadyQuiet(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWPodsReadyEx(ctx, aw, threeHundredSeconds, int(aw.Spec.Scheduling.MinAvailable), true)
}

func waitAWAnyPodsExists(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWPodsExists(ctx, aw, ninetySeconds)
}

func waitAWPodsExists(ctx *context, aw *arbv1.AppWrapper, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, anyPodsExist(ctx, aw.Namespace, aw.Name))
}

func waitAWDeleted(ctx *context, aw *arbv1.AppWrapper, pods []*v1.Pod) error {
	return waitAWPodsTerminatedEx(ctx, aw.Namespace, aw.Name, pods, 0)
}

func waitAWPodsDeleted(ctx *context, awNamespace string, awName string, pods []*v1.Pod) error {
	return waitAWPodsDeletedVerbose(ctx, awNamespace, awName, pods, true)
}

func waitAWPodsDeletedVerbose(ctx *context, awNamespace string, awName string, pods []*v1.Pod, verbose bool) error {
	return waitAWPodsTerminatedEx(ctx, awNamespace, awName, pods, 0)
}

func waitAWPending(ctx *context, aw *arbv1.AppWrapper) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending}, int(aw.Spec.Scheduling.MinAvailable), false))
}

func waitAWPodsReadyEx(ctx *context, aw *arbv1.AppWrapper, waitDuration time.Duration, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, waitDuration, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum, quite))
}

func waitAWPodsCompletedEx(ctx *context, aw *arbv1.AppWrapper, taskNum int, quite bool, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodSucceeded}, taskNum, quite))
}

func waitAWPodsNotCompletedEx(ctx *context, aw *arbv1.AppWrapper, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, threeMinutes, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending, v1.PodRunning, v1.PodFailed, v1.PodUnknown}, taskNum, quite))
}

func waitAWPodsPending(ctx *context, aw *arbv1.AppWrapper) error {
	return waitAWPodsPendingEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWPodsPendingEx(ctx *context, aw *arbv1.AppWrapper, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending}, taskNum, quite))
}

func waitAWPodsTerminatedEx(ctx *context, namespace string, name string, pods []*v1.Pod, taskNum int) error {
	return waitAWPodsTerminatedExVerbose(ctx, namespace, name, pods, taskNum, true)
}

func waitAWPodsTerminatedExVerbose(ctx *context, namespace string, name string, pods []*v1.Pod, taskNum int, verbose bool) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, podPhase(ctx, namespace, name, pods,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded, v1.PodUnknown, v1.PodFailed, v1.PodPending}, taskNum))
}

func createJobAWWithInitContainer(context *context, name string, requeuingTimeInSeconds int, requeuingGrowthType string, requeuingMaxNumRequeuings int) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "batch/v1",
		"kind": "Job",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"parallelism": 3,
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
				}
			},
			"spec": {
				"terminationGracePeriodSeconds": 1,
				"restartPolicy": "Never",
				"initContainers": [
					{
						"name": "job-init-container",
						"image": "quay.io/project-codeflare/busybox:latest",
						"command": ["sleep", "200"],
						"resources": {
							"requests": {
								"cpu": "500m"
							}
						}
					}
				],
				"containers": [
					{
						"name": "job-container",
						"image": "quay.io/project-codeflare/busybox:latest",
						"command": ["sleep", "10"],
						"resources": {
							"requests": {
								"cpu": "500m"
							}
						}
					}
				]
			}
		}
	}} `)

	var minAvailable int32 = 3

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: minAvailable,
				Requeuing: arbv1.RequeuingSpec{
					TimeInSeconds:      int64(requeuingTimeInSeconds),
					DoNotUseGrowthType: requeuingGrowthType,
					MaxNumRequeuings:   int32(requeuingMaxNumRequeuings),
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"replicas": 3,
		"selector": {
			"matchLabels": {
				"app": "` + name + `"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 3

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAWwith550CPU(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "` + name + `"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "550m"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAWwith350CPU(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
	"metadata": {
		"name": "aw-deployment-2-350cpu",
		"namespace": "test",
		"labels": {
			"app": "aw-deployment-2-350cpu"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "aw-deployment-2-350cpu"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-deployment-2-350cpu",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "aw-deployment-2-350cpu"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-deployment-2-350cpu",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "350m"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAWwith426CPU(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "` + name + `",
		"namespace": "test",
		"labels": {
			"app": "` + name + `"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "` + name + `"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "427m"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createDeploymentAWwith425CPU(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "aw-deployment-2-425cpu",
		"namespace": "test",
		"labels": {
			"app": "aw-deployment-2-425cpu"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "aw-deployment-2-425cpu"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-deployment-2-425cpu",
					"appwrapper.mcad.ibm.com": "aw-deployment-2-425cpu"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "aw-deployment-2-425cpu"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-deployment-2-425cpu",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "425m"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
	"metadata": {
		"name": "aw-generic-deployment-3",
		"namespace": "test",
		"labels": {
			"app": "aw-generic-deployment-3"
		}
	},
	"spec": {
		"replicas": 3,
		"selector": {
			"matchLabels": {
				"app": "aw-generic-deployment-3"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-generic-deployment-3",
					"appwrapper.mcad.ibm.com": "aw-generic-deployment-3"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "aw-generic-deployment-3"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-generic-deployment-3",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 3

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Progressing",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithStatus(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-job-with-comp-1"
					}
				},
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	// var schedSpecMin int = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				// MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithMultipleStatus(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-ms-21-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-job-with-comp-ms-21"
					}
				},
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-ms-21-1",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	rb2 := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-comp-ms-21-2",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-job-with-comp-ms-21"
					}
				},
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-ms-21-2",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb2,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createAWGenericItemWithoutStatus(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "scheduling.sigs.k8s.io/v1alpha1",
                        "kind": "PodGroup",
                        "metadata": {
                            "name": "aw-schd-spec-with-timeout-1",
                            "namespace": "default",
							"labels":{
								"appwrapper.mcad.ibm.com": "aw-test-job-with-comp-44"
							}
                        },
                        "spec": {
                            "minMember": 1
                        }
		}`)
	var schedSpecMin int32 = 1
	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWWithScheduleSpec(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-scheduling-spec",
			"namespace": "test"
		},
		"spec": {
			"completions": 2,
			"parallelism": 2,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-job-with-scheduling-spec"
					}
				},
				"spec": {
					"containers": [
						{
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"args": [
								"sleep 5"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-scheduling-spec",
							"resources": {
								"limits": {
									"cpu": "100m",
									"memory": "256M"
								},
								"requests": {
									"cpu": "100m",
									"memory": "256M"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)

	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericJobAWtWithLargeCompute(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "batch/v1",
		"kind": "Job",
		"metadata": {
			"name": "aw-test-job-with-large-comp-1",
			"namespace": "test"
		},
		"spec": {
			"completions": 1,
			"parallelism": 1,
			"template": {
				"metadata": {
					"labels": {
						"appwrapper.mcad.ibm.com": "aw-test-job-with-large-comp-1"
					}
				},
				"spec": {
					"containers": [
						{
							"args": [
								"sleep 5"
							],
							"command": [
								"/bin/bash",
								"-c",
								"--"
							],
							"image": "ubuntu:latest",
							"imagePullPolicy": "IfNotPresent",
							"name": "aw-test-job-with-comp-1",
							"resources": {
								"limits": {
									"cpu": "10000m",
									"memory": "256M",
									"nvidia.com/gpu": "100"
								},
								"requests": {
									"cpu": "100000m",
									"memory": "256M",
									"nvidia.com/gpu": "100"
								}
							}
						}
					],
					"restartPolicy": "Never"
				}
			}
		}
	}`)
	// var schedSpecMin int = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				// MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						// CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericServiceAWWithNoStatus(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Service",
		"metadata": {
			"labels": {
				"appwrapper.mcad.ibm.com": "test-dep-job-item",
				"resourceName": "test-dep-job-item-svc"
			},
			"name": "test-dep-job-item-svc",
			"namespace": "test"
		},
		"spec": {
			"ports": [
				{
					"name": "client",
					"port": 10001,
					"protocol": "TCP",
					"targetPort": 10001
				},
				{
					"name": "dashboard",
					"port": 8265,
					"protocol": "TCP",
					"targetPort": 8265
				},
				{
					"name": "redis",
					"port": 6379,
					"protocol": "TCP",
					"targetPort": 6379
				}
			],
			"selector": {
				"component": "test-dep-job-item-svc"
			},
			"sessionAffinity": "None",
			"type": "ClusterIP"
		}
	}`)
	// var schedSpecMin int = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				// MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Complete",
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentAWWithMultipleItems(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "` + name + `-deployment-1",
			"namespace": "test",
			"labels": {
				"app": "` + name + `-deployment-1"
			}
		},
		"spec": {
			"replicas": 1,
			"selector": {
				"matchLabels": {
					"app": "` + name + `-deployment-1"
				}
			},
			"template": {
				"metadata": {
					"labels": {
						"app": "` + name + `-deployment-1",
						"appwrapper.mcad.ibm.com": "` + name + `"
					},
					"annotations": {
						"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
					}
				},
				"spec": {
					"initContainers": [
						{
							"name": "job-init-container",
							"image": "quay.io/project-codeflare/busybox:latest",
							"command": ["sleep", "200"],
							"resources": {
								"requests": {
									"cpu": "500m"
								}
							}
						}
					],
					"containers": [
						{
							"name": "` + name + `-deployment-1",
							"image": "quay.io/project-codeflare/echo-server:1.0",
							"ports": [
								{
									"containerPort": 80
								}
							]
						}
					]
				}
			}
		}} `)
	rb1 := []byte(`{"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "` + name + `-deployment-2",
		"namespace": "test",
		"labels": {
			"app": "` + name + `-deployment-2"
		}
	},

	"spec": {
		"replicas": 1,
		"selector": {
			"matchLabels": {
				"app": "` + name + `-deployment-2"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "` + name + `-deployment-2",
					"appwrapper.mcad.ibm.com": "` + name + `"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "` + name + `"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "` + name + `-deployment-2",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)

	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
						CompletionStatus: "Progressing",
					},
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb1,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentWithCPUAW(context *context, name string, cpuDemand string, replicas int) *arbv1.AppWrapper {
	rb := []byte(fmt.Sprintf(`{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "%s",
		"namespace": "test",
		"labels": {
			"app": "%s"
		}
	},
	"spec": {
		"replicas": %d,
		"selector": {
			"matchLabels": {
				"app": "%s"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "%s",
					"appwrapper.mcad.ibm.com": "%s"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "%s"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `, name, name, replicas, name, name, name, name, name, cpuDemand))

	var schedSpecMin int32 = int32(replicas)

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericDeploymentCustomPodResourcesWithCPUAW(context *context, name string, customPodCpuDemand string, cpuDemand string, replicas int, requeuingTimeInSeconds int) *arbv1.AppWrapper {
	rb := []byte(fmt.Sprintf(`{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
		"name": "%s",
		"namespace": "test",
		"labels": {
			"app": "%s"
		}
	},
	"spec": {
		"replicas": %d,
		"selector": {
			"matchLabels": {
				"app": "%s"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "%s",
					"appwrapper.mcad.ibm.com": "%s"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "%s"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `, name, name, replicas, name, name, name, name, name, cpuDemand))

	var schedSpecMin int32 = int32(replicas)
	var customCpuResource = v1.ResourceList{"cpu": resource.MustParse(customPodCpuDemand)}

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
				Requeuing: arbv1.RequeuingSpec{
					TimeInSeconds: int64(requeuingTimeInSeconds),
				},
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						CustomPodResources: []arbv1.CustomPodResource{
							{
								Replicas: int32(replicas),
								Requests: customCpuResource,
							},
						},
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createNamespaceAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
		"kind": "Namespace",
	"metadata": {
		"name": "aw-namespace-0",
		"labels": {
			"app": "aw-namespace-0"
		}
	}} `)
	var schedSpecMin int32 = 0

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericNamespaceAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
		"kind": "Namespace",
	"metadata": {
		"name": "aw-generic-namespace-0",
		"labels": {
			"app": "aw-generic-namespace-0"
		}
	}} `)

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: arbv1.AppWrapperSpec{
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 0,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createStatefulSetAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "StatefulSet",
	"metadata": {
		"name": "aw-statefulset-2",
		"namespace": "test",
		"labels": {
			"app": "aw-statefulset-2"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "aw-statefulset-2"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-statefulset-2",
					"appwrapper.mcad.ibm.com/": "aw-statefulset-2"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "aw-statefulset-2"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-statefulset-2",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"imagePullPolicy": "Never",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericStatefulSetAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "apps/v1",
		"kind": "StatefulSet",
	"metadata": {
		"name": "aw-generic-statefulset-2",
		"namespace": "test",
		"labels": {
			"app": "aw-generic-statefulset-2"
		}
	},
	"spec": {
		"replicas": 2,
		"selector": {
			"matchLabels": {
				"app": "aw-generic-statefulset-2"
			}
		},
		"template": {
			"metadata": {
				"labels": {
					"app": "aw-generic-statefulset-2",
					"appwrapper.mcad.ibm.com": "aw-generic-statefulset-2"
				},
				"annotations": {
					"appwrapper.mcad.ibm.com/appwrapper-name": "aw-generic-statefulset-2"
				}
			},
			"spec": {
				"containers": [
					{
						"name": "aw-generic-statefulset-2",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"imagePullPolicy": "Never",
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
				]
			}
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 2,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}
	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

// NOTE:
//
//	Recommend this test not to be the last test in the test suite it may pass
//	the local test but may cause controller to fail which is not
//	part of this test's validation.
func createBadPodTemplateAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"labels": {
				"app": "aw-bad-podtemplate-2",
				"appwrapper.mcad.ibm.com": "aw-bad-podtemplate-2"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "aw-bad-podtemplate-2"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-bad-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 2,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createPodTemplateAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-podtemplate-1",
		"namespace": "test",
		"labels": {
			"appwrapper.mcad.ibm.com": "aw-podtemplate-2"
		}
	},
	"spec": {
			"containers": [
				{
					"name": "aw-podtemplate-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	rb1 := []byte(`{"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-podtemplate-2",
		"namespace": "test",
		"labels": {
			"appwrapper.mcad.ibm.com": "aw-podtemplate-2"
		}
	},
	"spec": {
			"containers": [
				{
					"name": "aw-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb1,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createPodCheckFailedStatusAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
	"apiVersion": "v1",
	"kind": "Pod",
	"metadata": {
		"name": "aw-checkfailedstatus-1",
		"namespace": "test",
		"labels": {
			"appwrapper.mcad.ibm.com": "aw-checkfailedstatus-1"
		}
	},
	"spec": {
			"containers": [
				{
					"name": "aw-checkfailedstatus-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			],
			"tolerations": [
				{
					"effect": "NoSchedule",
					"key": "key1",
					"value": "value1",
					"operator": "Equal"
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 1,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodAWCustomDemand(context *context, name string, cpuDemand string) *arbv1.AppWrapper {
	genericItems := fmt.Sprintf(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "%s",
			"namespace": "test",
			"labels": {
				"app": "%s",
				"appwrapper.mcad.ibm.com": "%s"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "%s"
			}
		},
		"spec": {
			"containers": [
					{
						"name": "%s",
						"image": "quay.io/project-codeflare/echo-server:1.0",
						"resources": {
							"limits": {
								"cpu": "%s"
							},
							"requests": {
								"cpu": "%s"
							}
						},
						"ports": [
							{
								"containerPort": 80
							}
						]
					}
			]
		}
	} `, name, name, name, name, name, cpuDemand, cpuDemand)

	rb := []byte(genericItems)
	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "aw-generic-pod-1",
			"namespace": "test",
			"labels": {
				"app": "aw-generic-pod-1",
				"appwrapper.mcad.ibm.com": "aw-generic-pod-1"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "aw-generic-pod-1"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-pod-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"resources": {
						"limits": {
							"memory": "150Mi"
						},
						"requests": {
							"memory": "150Mi"
						}
					},
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createGenericPodTooBigAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"name": "aw-generic-big-pod-1",
			"namespace": "test",
			"labels": {
				"app": "aw-generic-big-pod-1",
				"appwrapper.mcad.ibm.com": "aw-generic-big-pod-1"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "aw-generic-big-pod-1"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-big-pod-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"resources": {
						"limits": {
							"cpu": "100",
							"memory": "150Mi"
						},
						"requests": {
							"cpu": "100",
							"memory": "150Mi"
						}
					},
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)

	var schedSpecMin int32 = 1

	labels := make(map[string]string)
	labels["quota_service"] = "service-w"

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
			Labels:    labels,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadGenericPodAW(context *context, name string) *arbv1.AppWrapper {
	rb := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
			"labels": {
				"app": "aw-bad-generic-pod-1",
				"appwrapper.mcad.ibm.com": "aw-bad-generic-pod-1"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "aw-bad-generic-pod-1"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-bad-generic-pod-1",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	} `)
	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadGenericItemAW(context *context, name string) *arbv1.AppWrapper {
	// rb := []byte(`""`)
	var schedSpecMin int32 = 1

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						// GenericTemplate: runtime.RawExtension{
						// 	Raw: rb,
						// },
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).NotTo(HaveOccurred())

	return aw
}

func createBadGenericPodTemplateAW(context *context, name string) (*arbv1.AppWrapper, error) {
	rb := []byte(`{"metadata":
	{
		"name": "aw-generic-podtemplate-2",
		"namespace": "test",
		"labels": {
			"app": "aw-generic-podtemplate-2"
		}
	},
	"template": {
		"metadata": {
			"labels": {
				"app": "aw-generic-podtemplate-2",
				"appwrapper.mcad.ibm.com": "aw-generic-podtemplate-2"
			},
			"annotations": {
				"appwrapper.mcad.ibm.com/appwrapper-name": "aw-generic-podtemplate-2"
			}
		},
		"spec": {
			"containers": [
				{
					"name": "aw-generic-podtemplate-2",
					"image": "quay.io/project-codeflare/echo-server:1.0",
					"ports": [
						{
							"containerPort": 80
						}
					]
				}
			]
		}
	}} `)
	var schedSpecMin int32 = 2

	aw := &arbv1.AppWrapper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: context.namespace,
		},
		Spec: arbv1.AppWrapperSpec{
			Scheduling: arbv1.SchedulingSpec{
				MinAvailable: schedSpecMin,
			},
			Resources: arbv1.AppWrapperResources{
				GenericItems: []arbv1.GenericItem{
					{
						DoNotUseReplicas: 2,
						GenericTemplate: runtime.RawExtension{
							Raw: rb,
						},
					},
				},
			},
		},
	}

	err := context.client.Create(context.ctx, aw)
	Expect(err).To(HaveOccurred())
	return aw, err
}

func deleteAppWrapper(ctx *context, name string) error {
	foreground := metav1.DeletePropagationForeground
	aw := &arbv1.AppWrapper{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: ctx.namespace,
	}}
	return ctx.client.Delete(ctx.ctx, aw, &client.DeleteOptions{PropagationPolicy: &foreground})

}

func getPodsOfAppWrapper(ctx *context, aw *arbv1.AppWrapper) []*v1.Pod {
	awIgnored := &arbv1.AppWrapper{}
	err := ctx.client.Get(ctx.ctx, client.ObjectKeyFromObject(aw), awIgnored) // TODO: Do we actually need to do this Get?
	Expect(err).NotTo(HaveOccurred())

	pods := &v1.PodList{}
	err = ctx.client.List(gcontext.Background(), pods, &client.ListOptions{Namespace: aw.Namespace})
	Expect(err).NotTo(HaveOccurred())

	var awpods []*v1.Pod

	for index := range pods.Items {
		// Get a pointer to the pod in the list not a pointer to the podCopy
		pod := &pods.Items[index]

		if gn, found := pod.Annotations["appwrapper.mcad.ibm.com/appwrapper-name"]; !found || gn != aw.Name {
			continue
		}
		awpods = append(awpods, pod)
	}

	return awpods
}

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

func appendRandomString(value string) string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return fmt.Sprintf("%s-%s", value, string(b))
}

func AppWrapper(context *context, namespace string, name string) func(g gomega.Gomega) *arbv1.AppWrapper {
	return func(g gomega.Gomega) *arbv1.AppWrapper {
		aw := &arbv1.AppWrapper{}
		err := context.client.Get(context.ctx, client.ObjectKey{Namespace: namespace, Name: name}, aw)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return aw
	}
}

/* TODO: DAVE AppWrapperState
func AppWrapperState(aw *arbv1.AppWrapper) arbv1.AppWrapperState {
	return aw.Status.State
}
*/

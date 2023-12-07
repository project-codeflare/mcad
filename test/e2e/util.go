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
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
)

const testNamespace = "test"

var ninetySeconds = 90 * time.Second
var threeMinutes = 180 * time.Second
var tenMinutes = 600 * time.Second
var threeHundredSeconds = 300 * time.Second

type myKey struct {
	key string
}

func getClient(ctx context.Context) client.Client {
	kubeClient := ctx.Value(myKey{key: "kubeclient"})
	return kubeClient.(client.Client)
}

func extendContextWithClient(ctx context.Context) context.Context {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = arbv1.AddToScheme(scheme)
	kubeclient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	return context.WithValue(ctx, myKey{key: "kubeclient"}, kubeclient)
}

func ensureNamespaceExists(ctx context.Context) {
	err := getClient(ctx).Create(ctx, &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	})
	Expect(client.IgnoreAlreadyExists(err)).NotTo(HaveOccurred())
}

func cleanupTestObjectsPtr(ctx context.Context, appwrappersPtr *[]*arbv1.AppWrapper) {
	cleanupTestObjectsPtrVerbose(ctx, appwrappersPtr, true)
}

func cleanupTestObjectsPtrVerbose(ctx context.Context, appwrappersPtr *[]*arbv1.AppWrapper, verbose bool) {
	if appwrappersPtr == nil {
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjectsPtr] No  AppWrappers to cleanup.\n")
	} else {
		cleanupTestObjects(ctx, *appwrappersPtr)
	}
}

func cleanupTestObjects(ctx context.Context, appwrappers []*arbv1.AppWrapper) {
	cleanupTestObjectsVerbose(ctx, appwrappers, true)
}

func cleanupTestObjectsVerbose(ctx context.Context, appwrappers []*arbv1.AppWrapper, verbose bool) {
	if appwrappers == nil {
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] No AppWrappers to cleanup.\n")
		return
	}

	for _, aw := range appwrappers {
		pods := getPodsOfAppWrapper(ctx, aw)
		awNamespace := aw.Namespace
		awName := aw.Name
		fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Deleting AW %s.\n", aw.Name)
		err := deleteAppWrapper(ctx, aw.Name, aw.Namespace)
		Expect(err).NotTo(HaveOccurred())

		// Wait for the pods of the deleted the appwrapper to be destroyed
		for _, pod := range pods {
			fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Awaiting pod %s/%s to be deleted for AW %s.\n",
				pod.Namespace, pod.Name, aw.Name)
		}
		err = waitAWPodsDeleted(ctx, awNamespace, awName, pods)

		// Final check to see if pod exists
		if err != nil {
			var podsStillExisting []*v1.Pod
			for _, pod := range pods {
				podExist := &v1.Pod{}
				err = getClient(ctx).Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, podExist)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "[cleanupTestObjects] Found pod %s/%s %s, not completedly deleted for AW %s.\n", podExist.Namespace, podExist.Name, podExist.Status.Phase, aw.Name)
					podsStillExisting = append(podsStillExisting, podExist)
				}
			}
			if len(podsStillExisting) > 0 {
				err = waitAWPodsDeleted(ctx, awNamespace, awName, podsStillExisting)
			}
		}
		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteAppWrapper(ctx context.Context, name string, namespace string) error {
	foreground := metav1.DeletePropagationForeground
	aw := &arbv1.AppWrapper{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}}
	return getClient(ctx).Delete(ctx, aw, &client.DeleteOptions{PropagationPolicy: &foreground})
}

func anyPodsExist(ctx context.Context, awNamespace string, awName string) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := getClient(ctx).List(context.Background(), podList, &client.ListOptions{Namespace: awNamespace})
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

func podPhase(ctx context.Context, awNamespace string, awName string, pods []*v1.Pod, phase []v1.PodPhase, taskNum int) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := getClient(ctx).List(context.Background(), podList, &client.ListOptions{Namespace: awNamespace})
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

func awPodPhase(ctx context.Context, aw *arbv1.AppWrapper, phase []v1.PodPhase, taskNum int, quite bool) wait.ConditionFunc {
	return func() (bool, error) {
		defer GinkgoRecover()
		awIgnored := &arbv1.AppWrapper{}
		err := getClient(ctx).Get(ctx, client.ObjectKey{Namespace: aw.Namespace, Name: aw.Name}, awIgnored) // TODO: Do we actually need to do this Get?
		Expect(err).NotTo(HaveOccurred())

		podList := &v1.PodList{}
		err = getClient(ctx).List(ctx, podList, &client.ListOptions{Namespace: aw.Namespace})
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

func waitAWPodsReady(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsReadyEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWPodsCompleted(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration) error {
	return waitAWPodsCompletedEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false, timeout)
}

func waitAWPodsNotCompleted(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsNotCompletedEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWReadyQuiet(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsReadyEx(ctx, aw, threeHundredSeconds, int(aw.Spec.Scheduling.MinAvailable), true)
}

func waitAWAnyPodsExists(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsExists(ctx, aw, ninetySeconds)
}

func waitAWPodsExists(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, anyPodsExist(ctx, aw.Namespace, aw.Name))
}

func waitAWDeleted(ctx context.Context, aw *arbv1.AppWrapper, pods []*v1.Pod) error {
	return waitAWPodsTerminatedEx(ctx, aw.Namespace, aw.Name, pods, 0)
}

func waitAWPodsDeleted(ctx context.Context, awNamespace string, awName string, pods []*v1.Pod) error {
	return waitAWPodsDeletedVerbose(ctx, awNamespace, awName, pods, true)
}

func waitAWPodsDeletedVerbose(ctx context.Context, awNamespace string, awName string, pods []*v1.Pod, verbose bool) error {
	return waitAWPodsTerminatedEx(ctx, awNamespace, awName, pods, 0)
}

func waitAWPending(ctx context.Context, aw *arbv1.AppWrapper) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending}, int(aw.Spec.Scheduling.MinAvailable), false))
}

func waitAWPodsReadyEx(ctx context.Context, aw *arbv1.AppWrapper, waitDuration time.Duration, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, waitDuration, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded}, taskNum, quite))
}

func waitAWPodsCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, taskNum int, quite bool, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodSucceeded}, taskNum, quite))
}

func waitAWPodsNotCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, threeMinutes, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending, v1.PodRunning, v1.PodFailed, v1.PodUnknown}, taskNum, quite))
}

func waitAWPodsPending(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsPendingEx(ctx, aw, int(aw.Spec.Scheduling.MinAvailable), false)
}

func waitAWPodsPendingEx(ctx context.Context, aw *arbv1.AppWrapper, taskNum int, quite bool) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, awPodPhase(ctx, aw,
		[]v1.PodPhase{v1.PodPending}, taskNum, quite))
}

func waitAWPodsTerminatedEx(ctx context.Context, namespace string, name string, pods []*v1.Pod, taskNum int) error {
	return waitAWPodsTerminatedExVerbose(ctx, namespace, name, pods, taskNum, true)
}

func waitAWPodsTerminatedExVerbose(ctx context.Context, namespace string, name string, pods []*v1.Pod, taskNum int, verbose bool) error {
	return wait.Poll(100*time.Millisecond, ninetySeconds, podPhase(ctx, namespace, name, pods,
		[]v1.PodPhase{v1.PodRunning, v1.PodSucceeded, v1.PodUnknown, v1.PodFailed, v1.PodPending}, taskNum))
}

func getPodsOfAppWrapper(ctx context.Context, aw *arbv1.AppWrapper) []*v1.Pod {
	awIgnored := &arbv1.AppWrapper{}
	err := getClient(ctx).Get(ctx, client.ObjectKeyFromObject(aw), awIgnored) // TODO: Do we actually need to do this Get?
	Expect(err).NotTo(HaveOccurred())

	pods := &v1.PodList{}
	err = getClient(ctx).List(context.Background(), pods, &client.ListOptions{Namespace: aw.Namespace})
	Expect(err).NotTo(HaveOccurred())

	var awpods []*v1.Pod

	for index := range pods.Items {
		// Get a pointer to the pod in the list not a pointer to the podCopy
		pod := &pods.Items[index]

		if gn, found := pod.Labels["appwrapper.mcad.ibm.com"]; !found || gn != aw.Name {
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

func AppWrapper(ctx context.Context, namespace string, name string) func(g gomega.Gomega) *arbv1.AppWrapper {
	return func(g gomega.Gomega) *arbv1.AppWrapper {
		aw := &arbv1.AppWrapper{}
		err := getClient(ctx).Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, aw)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return aw
	}
}

/* TODO: DAVE AppWrapperState
func AppWrapperState(aw *arbv1.AppWrapper) arbv1.AppWrapperState {
	return aw.Status.State
}
*/

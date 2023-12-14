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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	arbv1 "github.com/project-codeflare/mcad/api/v1beta1"
	arcont "github.com/project-codeflare/mcad/internal/controller"
)

const testNamespace = "test"

var ninetySeconds = 90 * time.Second
var threeMinutes = 180 * time.Second
var tenMinutes = 600 * time.Second
var threeHundredSeconds = 300 * time.Second
var clusterCapacity v1.ResourceList = v1.ResourceList{}

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

// Compute available cluster capacity to allow tests to scale their resources appropriately.
// The code is a simplification of AppWrapperReconciler.computeCapacity and intended to be run
// in BeforeSuite methods (thus it is not necessary to filter out AppWrapper-owned pods)
func updateClusterCapacity(ctx context.Context) {
	kc := getClient(ctx)
	capacity := arcont.Weights{}
	// add allocatable capacity for each schedulable node
	nodes := &v1.NodeList{}
	err := kc.List(ctx, nodes)
	Expect(err).NotTo(HaveOccurred())

LOOP:
	for _, node := range nodes.Items {
		// skip unschedulable nodes
		if node.Spec.Unschedulable {
			continue
		}
		for _, taint := range node.Spec.Taints {
			if taint.Effect == v1.TaintEffectNoSchedule || taint.Effect == v1.TaintEffectNoExecute {
				continue LOOP
			}
		}
		// add allocatable capacity on the node
		capacity.Add(arcont.NewWeights(node.Status.Allocatable))
	}
	// subtract requests from non-terminated pods
	pods := &v1.PodList{}
	err = kc.List(ctx, pods)
	Expect(err).NotTo(HaveOccurred())
	for _, pod := range pods.Items {
		if pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded {
			capacity.Sub(arcont.NewWeightsForPod(&pod))
		}
	}

	clusterCapacity = capacity.AsResources()

	t, _ := json.Marshal(clusterCapacity)
	fmt.Fprintf(os.Stdout, "Computed cluster capacity: %v\n", string(t))
}

func cpuDemand(fractionOfCluster float64) *resource.Quantity {
	clusterCPU := clusterCapacity[v1.ResourceCPU]
	milliDemand := int64(float64(clusterCPU.MilliValue()) * fractionOfCluster)
	return resource.NewMilliQuantity(milliDemand, resource.DecimalSI)
}

func cleanupTestObjects(ctx context.Context, appwrappers []*arbv1.AppWrapper) {
	if appwrappers == nil {
		return
	}

	for _, aw := range appwrappers {
		awNamespace := aw.Namespace
		awName := aw.Name
		err := deleteAppWrapper(ctx, aw.Name, aw.Namespace)
		Expect(err).NotTo(HaveOccurred())
		err = waitAWPodsDeleted(ctx, awNamespace, awName)
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
		if err != nil {
			return false, err
		}

		for _, podFromPodList := range podList.Items {
			if awn, found := podFromPodList.Labels["appwrapper.mcad.ibm.com"]; found && awn == awName {
				return true, nil
			}
		}
		return false, nil
	}
}

func noPodsExist(ctx context.Context, awNamespace string, awName string) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := getClient(ctx).List(context.Background(), podList, &client.ListOptions{Namespace: awNamespace})
		if err != nil {
			return false, err
		}

		for _, podFromPodList := range podList.Items {
			if awn, found := podFromPodList.Labels["appwrapper.mcad.ibm.com"]; found && awn == awName {
				return false, nil
			}
		}
		return true, nil
	}
}

func podsInPhase(ctx context.Context, awNamespace string, awName string, phase []v1.PodPhase, minimumPodCount int) wait.ConditionFunc {
	return func() (bool, error) {
		podList := &v1.PodList{}
		err := getClient(ctx).List(ctx, podList, &client.ListOptions{Namespace: awNamespace})
		if err != nil {
			return false, err
		}

		matchingPodCount := 0
		for _, pod := range podList.Items {
			if awn, found := pod.Labels["appwrapper.mcad.ibm.com"]; found && awn == awName {
				for _, p := range phase {
					if pod.Status.Phase == p {
						matchingPodCount++
						break
					}
				}
			}
		}

		return minimumPodCount <= matchingPodCount, nil
	}
}

func waitAWPodsReady(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsReadyEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable))
}

func waitAWPodsCompleted(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsCompletedEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable))
}

func waitAWPodsNotCompleted(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsNotCompletedEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable))
}

func waitAWAnyPodsExists(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWAnyPodsExistsEx(ctx, aw, ninetySeconds)
}

func waitAWPodsDeleted(ctx context.Context, awNamespace string, awName string) error {
	return waitAWPodsDeletedEx(ctx, awNamespace, awName, ninetySeconds)
}

func waitAWPodsPending(ctx context.Context, aw *arbv1.AppWrapper) error {
	return waitAWPodsPendingEx(ctx, aw, ninetySeconds, int(aw.Spec.Scheduling.MinAvailable))
}

func waitAWAnyPodsExistsEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, anyPodsExist(ctx, aw.Namespace, aw.Name))
}

func waitAWPodsDeletedEx(ctx context.Context, awNamespace string, awName string, timeout time.Duration) error {
	return wait.Poll(100*time.Millisecond, timeout, noPodsExist(ctx, awNamespace, awName))
}

func waitAWPodsReadyEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}
	return wait.Poll(100*time.Millisecond, timeout, podsInPhase(ctx, aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodSucceeded}
	return wait.Poll(100*time.Millisecond, timeout, podsInPhase(ctx, aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsNotCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodPending, v1.PodRunning, v1.PodFailed, v1.PodUnknown}
	return wait.Poll(100*time.Millisecond, timeout, podsInPhase(ctx, aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsPendingEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodPending}
	return wait.Poll(100*time.Millisecond, timeout, podsInPhase(ctx, aw.Namespace, aw.Name, phases, taskNum))
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

func AppWrapperState(ctx context.Context, namespace string, name string) func(g gomega.Gomega) arbv1.AppWrapperState {
	return func(g gomega.Gomega) arbv1.AppWrapperState {
		aw := &arbv1.AppWrapper{}
		err := getClient(ctx).Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, aw)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return aw.Status.State
	}
}

func AppWrapperStep(ctx context.Context, namespace string, name string) func(g gomega.Gomega) arbv1.AppWrapperStep {
	return func(g gomega.Gomega) arbv1.AppWrapperStep {
		aw := &arbv1.AppWrapper{}
		err := getClient(ctx).Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, aw)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return aw.Status.Step
	}
}

func AppWrapperQueuedReason(ctx context.Context, namespace string, name string) func(g gomega.Gomega) string {
	return func(g gomega.Gomega) string {
		aw := &arbv1.AppWrapper{}
		err := getClient(ctx).Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, aw)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if qc := meta.FindStatusCondition(aw.Status.Conditions, string(arbv1.Queued)); qc != nil && qc.Status == metav1.ConditionTrue {
			return qc.Reason
		} else {
			return ""
		}
	}
}

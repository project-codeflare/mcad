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
	"time"

	. "github.com/onsi/ginkgo/v2"
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
)

const testNamespace = "test"

const ninetySeconds = 90 * time.Second

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

// Update available cluster capacity to allow tests to scale their resources appropriately.
func updateClusterCapacity(ctx context.Context) {
	clusters := &arbv1.ClusterInfoList{}
	err := getClient(ctx).List(ctx, clusters)
	Expect(err).NotTo(HaveOccurred())
	// TODO: Multi-cluster.  Assuming a single cluster here.
	Expect(len(clusters.Items)).Should(Equal(1))
	clusterCapacity = clusters.Items[0].Status.Capacity.DeepCopy()
	t, _ := json.Marshal(clusterCapacity)
	fmt.Fprintf(GinkgoWriter, "Computed cluster capacity: %v\n", string(t))
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

func anyPodsExist(awNamespace string, awName string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
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

func noPodsExist(awNamespace string, awName string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
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

func podsInPhase(awNamespace string, awName string, phase []v1.PodPhase, minimumPodCount int) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
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
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, anyPodsExist(aw.Namespace, aw.Name))
}

func waitAWPodsDeletedEx(ctx context.Context, awNamespace string, awName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, noPodsExist(awNamespace, awName))
}

func waitAWPodsReadyEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodRunning, v1.PodSucceeded}
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, podsInPhase(aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodSucceeded}
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, podsInPhase(aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsNotCompletedEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodPending, v1.PodRunning, v1.PodFailed, v1.PodUnknown}
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, podsInPhase(aw.Namespace, aw.Name, phases, taskNum))
}

func waitAWPodsPendingEx(ctx context.Context, aw *arbv1.AppWrapper, timeout time.Duration, taskNum int) error {
	phases := []v1.PodPhase{v1.PodPending}
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, podsInPhase(aw.Namespace, aw.Name, phases, taskNum))
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

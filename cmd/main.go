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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"

	workloadv1alpha1 "github.com/project-codeflare/mcad/api/v1alpha1"
	mcadv1beta1 "github.com/project-codeflare/mcad/api/v1beta1"
	"github.com/project-codeflare/mcad/internal/controller"
	mcad_kueue "github.com/project-codeflare/mcad/internal/kueue"
	//+kubebuilder:scaffold:imports
)

var (
	scheme       = runtime.NewScheme()
	setupLog     = ctrl.Log.WithName("setup")
	BuildVersion = "UNKNOWN"
	BuildDate    = "UNKNOWN"
)

const (
	UnifiedMode    = "unified"
	DispatcherMode = "dispatcher"
	RunnerMode     = "runner"
	KueueMode      = "kueue"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(mcadv1beta1.AddToScheme(scheme))
	utilruntime.Must(workloadv1alpha1.AddToScheme(scheme))
	utilruntime.Must(kueue.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var multicluster bool
	var mode string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&mode, "mode", UnifiedMode,
		"One of "+UnifiedMode+", "+DispatcherMode+", "+RunnerMode+" or "+KueueMode+".")
	flag.BoolVar(&multicluster, "multicluster", false, "Enable multi-cluster operation")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog.Info("Build info", "mcadVersion", BuildVersion, "date", BuildDate)

	if mode != UnifiedMode && mode != RunnerMode && mode != DispatcherMode && mode != KueueMode {
		setupLog.Error(nil, fmt.Sprintf("invalid mode: %v", mode))
		os.Exit(1)
	}

	var leaderElectionID string
	if mode == UnifiedMode || mode == DispatcherMode {
		leaderElectionID = "f933c2fb.codeflare.dev"
	} else {
		leaderElectionID = "a69497ab.codeflare.dev"
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if mode == UnifiedMode || mode == DispatcherMode {
		if err = (&controller.Dispatcher{
			AppWrapperReconciler: controller.AppWrapperReconciler{
				Client:           mgr.GetClient(),
				Scheme:           mgr.GetScheme(),
				Cache:            map[types.UID]*controller.CachedAppWrapper{}, // AppWrapper cache
				MultiClusterMode: multicluster,
				ControllerName:   "Dispatcher",
			},
			Decisions: map[types.UID]*controller.QueuingDecision{}, // cache of recent queuing decisions
			Events:    make(chan event.GenericEvent, 1),            // channel to trigger dispatch,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Dispatcher")
			os.Exit(1)
		}
	}

	if mode == UnifiedMode || mode == RunnerMode {
		if err = (&controller.Runner{
			AppWrapperReconciler: controller.AppWrapperReconciler{
				Client:           mgr.GetClient(),
				Scheme:           mgr.GetScheme(),
				Cache:            map[types.UID]*controller.CachedAppWrapper{}, // AppWrapper cache
				MultiClusterMode: multicluster,
				ControllerName:   "Runner",
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Runner")
			os.Exit(1)
		}

		if err = (&controller.ClusterInfoReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ClusterInfo")
			os.Exit(1)
		}
	}

	if mode == KueueMode {
		if err := (&mcad_kueue.BoxedJobReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "BoxedJob")
			os.Exit(1)
		}

		if err := mcad_kueue.NewReconciler(
			mgr.GetClient(),
			mgr.GetEventRecorderFor("kueue-boxedjob"),
		).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Kueue")
			os.Exit(1)
		}

		wh := &mcad_kueue.BoxedJobWebhook{ManageJobsWithoutQueueName: true}
		if err := ctrl.NewWebhookManagedBy(mgr).
			For(&workloadv1alpha1.BoxedJob{}).
			WithDefaulter(wh).
			WithValidator(wh).
			Complete(); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "boxedjob")
			os.Exit(1)
		}

		ctx := context.TODO() // TODO: figure out the right context to use here!
		if err := jobframework.SetupWorkloadOwnerIndex(ctx, mgr.GetFieldIndexer(), mcad_kueue.GVK); err != nil {
			setupLog.Error(err, "Setting up indexes", "GVK", mcad_kueue.GVK)
			os.Exit(1)
		}
	}

	//+kubebuilder:scaffold:builder
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

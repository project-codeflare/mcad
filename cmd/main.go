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
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	mcadv1beta1 "github.com/tayebehbahreini/mcad/api/v1beta1"
	"github.com/tayebehbahreini/mcad/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(mcadv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var mode string
	var namespace string
	var name string
	var context string
	var geolocation string
	var powerslope string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	flag.StringVar(&mode, "mode", "default", "One of default, dispatcher, runner.")
	flag.StringVar(&namespace, "clusterinfo-namespace", "default", "The namespace of the ClusterInfo object")
	flag.StringVar(&name, "clusterinfo-name", controller.DefaultClusterName, "The name of the ClusterInfo object.")
	flag.StringVar(&context, "kube-context", "", "The Kubernetes context.")
	flag.StringVar(&geolocation, "geolocation", "US-NY-NYIS", "The geolocation of cluster.")
	flag.StringVar(&powerslope, "powerslope", "1", "The slope in power function.")
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	conf, err := config.GetConfigWithContext(context)
	if err != nil {
		setupLog.Error(err, "unable to get kubeconfig")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(conf, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f933c2fb.codeflare.dev",
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

	if mode != "dispatcher" {
		if msgs := validation.IsQualifiedName(namespace + "/" + name); len(msgs) > 0 {
			setupLog.Error(err, "invalid ClusterInfo namespace/name")
			os.Exit(1)
		}
		if err = (&controller.Runner{
			AppWrapperReconciler: controller.AppWrapperReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
				Cache:  map[types.UID]*controller.CachedAppWrapper{}, // AppWrapper cache
			},
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create AppWrapper runner")
			os.Exit(1)
		}
		if err = (&controller.ClusterInfoReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			Namespace:   namespace,
			Name:        name,
			Geolocation: geolocation,
			PowerSlope:  powerslope,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create ClusterInfo controller")
			os.Exit(1)
		}
	}

	if mode != "runner" {
		if err = (&controller.Dispatcher{
			AppWrapperReconciler: controller.AppWrapperReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
				Cache:  map[types.UID]*controller.CachedAppWrapper{}, // AppWrapper cache
			},
			Events: make(chan event.GenericEvent, 1), // channel to trigger dispatch
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create AppWrapper dispatcher")
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

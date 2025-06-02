package main

import (
	"log"
	"os"

	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	testcontext "github.com/openshift-splat-team/test-monitor/pkg/context"
	"github.com/openshift-splat-team/test-monitor/pkg/controller"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
)

func main() {
	logger := textlogger.NewLogger(textlogger.NewConfig())
	ctrl.SetLogger(logger)

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		log.Printf("could not create manager: %v", err)
		os.Exit(1)
	}

	err = v1.AddToScheme(mgr.GetScheme())
	if err != nil {
		log.Printf("could not add types to scheme: %v", err)
		os.Exit(1)
	}

	leaseReconciler := &controller.LeaseReconciler{}
	podReconciler := &controller.PodReconciler{}
	testContext := &testcontext.TestContextService{}
	testContext.Initialize()

	if err := leaseReconciler.
		SetupWithManager(mgr, testContext); err != nil {
		log.Printf("unable to create lease controller: %v", err)
		os.Exit(1)
	}
	if err := podReconciler.
		SetupWithManager(mgr, testContext); err != nil {
		log.Printf("unable to create pool controller: %v", err)
		os.Exit(1)
	}

	if err := (&controller.NamespaceReconciler{}).
		SetupWithManager(mgr, leaseReconciler, podReconciler, testContext); err != nil {
		log.Printf("unable to create namespace controller: %v", err)
		os.Exit(1)
	}

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Printf("could not start manager: %v", err)
		os.Exit(1)
	}

}

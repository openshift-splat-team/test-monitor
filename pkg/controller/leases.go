package controller

import (
	"context"
	"fmt"
	"sync"

	testcontext "github.com/openshift-splat-team/test-monitor/pkg/context"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LeaseReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	RESTMapper     meta.RESTMapper
	UncachedClient client.Client

	// Namespace is the namespace in which the ControlPlaneMachineSet controller should operate.
	// Any ControlPlaneMachineSet not in this namespace should be ignored.
	Namespace string

	// OperatorName is the name of the ClusterOperator with which the controller should report
	// its status.
	OperatorName string

	// ReleaseVersion is the version of current cluster operator release.
	ReleaseVersion string

	ctx context.Context

	testContext *testcontext.TestContextService
	mutex       *sync.Mutex
}

func (l *LeaseReconciler) SetupWithManager(mgr ctrl.Manager,
	testContext *testcontext.TestContextService) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Lease{}).
		Complete(l); err != nil {
		return fmt.Errorf("error setting up controller: %w", err)
	}
	l.mutex = &sync.Mutex{}

	l.ctx = context.Background()
	l.testContext = testContext

	// Set up API helpers from the manager.
	l.Client = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("leases-controller")
	l.RESTMapper = mgr.GetRESTMapper()
	return nil
}

func (l *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var lease v1.Lease
	err = l.Client.Get(l.ctx, req.NamespacedName, &lease)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting namespace: %w", err)
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	if lease.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if lease.Labels == nil {
		return ctrl.Result{}, nil
	}
	labels := lease.Labels
	var namespace string
	var exists bool

	if namespace, exists = labels["vsphere-capacity-manager.splat-team.io/lease-namespace"]; !exists {
		return ctrl.Result{}, nil
	}

	l.testContext.UpdateWithLease(corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, lease)

	return ctrl.Result{}, nil
}

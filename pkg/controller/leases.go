package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
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

	inited 		bool
	log 			logr.Logger
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
	l.log = mgr.GetLogger()

	return nil
}

func (l *LeaseReconciler) Initialize() error {
	if l.inited {
		return nil
	}
	l.inited = true

	leaseList := &v1.LeaseList{}
	err := l.Client.List(l.ctx, leaseList, &client.ListOptions{Namespace: "vsphere-infra-helpers"})
	if err != nil {
		return fmt.Errorf("error listing leases: %w", err)
	}

	for _, lease := range leaseList.Items {
		err = l.handleLease(lease)
		if err != nil {
			return fmt.Errorf("error handling lease: %w", err)
		}
	}
	return nil
}

func (l *LeaseReconciler) handleLease(lease v1.Lease) error {
	l.log.Info("handling lease","lease", lease.Name)

	if lease.DeletionTimestamp != nil {
		l.log.Info("lease is being deleted", "lease", lease.Name)
		return nil
	}

	if lease.Labels == nil {
		l.log.Info("lease has no labels", "lease", lease.Name)
		return nil
	}
	labels := lease.Labels
	var namespace string
	var exists bool

	if namespace, exists = labels["vsphere-capacity-manager.splat-team.io/lease-namespace"]; !exists {
		l.log.Info("lease lacks namespace label", "lease", lease.Name)
		return nil
	}

	l.testContext.UpdateWithLease(corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, lease)

	return nil
}

func (l *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var lease v1.Lease
	err = l.Client.Get(l.ctx, req.NamespacedName, &lease)
	if err != nil {
		l.log.Error(err, "error getting lease")		
		return ctrl.Result{}, nil
	}

	err = l.Initialize()
	if err != nil {
		l.log.Error(err, "error initializing")		
		return ctrl.Result{}, nil
	}

	err = l.handleLease(lease)
	if err != nil {
		l.log.Error(err, "error handling lease")
		return ctrl.Result{}, nil	}

	return ctrl.Result{}, nil
}

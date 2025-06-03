package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	testcontext "github.com/openshift-splat-team/test-monitor/pkg/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	BoskosIdLabel = "boskos-lease-id"
)

type NamespaceReconciler struct {
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

	//namespaces map[string]corev1.Namespace

	mutex sync.Mutex

	testContextService *testcontext.TestContextService

	log 			logr.Logger
}

func (l *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager,
	leaseController *LeaseReconciler,
	podController *PodReconciler,
	testContext *testcontext.TestContextService) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(l); err != nil {
		return fmt.Errorf("error setting up controller: %w", err)
	}

	l.testContextService = testContext

	l.ctx = context.Background()

	// Set up API helpers from the manager.
	l.Client = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("namespaces-controller")
	l.RESTMapper = mgr.GetRESTMapper()
	l.log = mgr.GetLogger()

	return nil
}

func (l *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var namespace corev1.Namespace
	err = l.Client.Get(l.ctx, req.NamespacedName, &namespace)
	if err != nil {
		return ctrl.Result{}, nil
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	if namespace.DeletionTimestamp != nil {

		testContext := l.testContextService.DestroyContext(namespace)
		promLabels, err := l.testContextService.GetPromLabelValues(testContext)
		if err != nil {
			l.log.Error(err, "error getting prom labels")			
			return ctrl.Result{}, nil
		} else {
			l.log.Info("namespace is being deleted", "namespace", namespace.Name, "failed", testContext.Failed, "prom labels", promLabels)
		}		
		if testContext.Failed {
			l.testContextService.Fail(testContext)
		} else {
			l.testContextService.Pass(testContext)
		}
		return ctrl.Result{}, nil
	}
	l.testContextService.UpdateWithNamespace(namespace)
	return ctrl.Result{}, nil
}

package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	testcontext "github.com/openshift-splat-team/test-monitor/pkg/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodReconciler struct {
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

	mutex *sync.Mutex

	log logr.Logger
}

func (l *PodReconciler) SetupWithManager(mgr ctrl.Manager,
	testContext *testcontext.TestContextService) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(l); err != nil {
		return fmt.Errorf("error setting up controller: %w", err)
	}
	l.mutex = &sync.Mutex{}

	l.ctx = context.Background()

	l.testContext = testContext

	// Set up API helpers from the manager.
	l.Client = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("pods-controller")
	l.RESTMapper = mgr.GetRESTMapper()
	l.log = mgr.GetLogger()

	return nil
}

func (l *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var pod corev1.Pod
	err = l.Client.Get(l.ctx, req.NamespacedName, &pod)
	if err != nil {
		return ctrl.Result{}, nil
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()	
	if pod.Status.Phase == corev1.PodFailed && strings.Contains(pod.Namespace, "ci-") {
		l.testContext.UpdateWithPods(corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: req.Namespace,
			},
		}, pod)
	}

	return ctrl.Result{}, nil
}

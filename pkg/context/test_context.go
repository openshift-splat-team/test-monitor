package context

import (
	"fmt"
	"path"
	"sync"

	"github.com/go-logr/logr"
	"github.com/openshift-splat-team/test-monitor/pkg/data"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	corev1 "k8s.io/api/core/v1"
)

const testContextsFilename = "test_contexts.json"

type TestContextService struct {
	testContexts   map[string]*data.TestContext
	metricsContext *MetricsContext
	mutex          *sync.Mutex
	log 			logr.Logger
}

func (t *TestContextService) Initialize(log logr.Logger) {
	t.testContexts = make(map[string]*data.TestContext)
	t.mutex = &sync.Mutex{}
	t.log = log
	t.metricsContext = &MetricsContext{}
	t.metricsContext.Initialize()
}

// getTestContext gets(or creates) the test context for a given namespace
func (t *TestContextService) getTestContext(namespace corev1.Namespace) *data.TestContext {
	var testContext *data.TestContext
	var present bool

	if testContext, present = t.testContexts[namespace.Name]; !present {
		testContext = &data.TestContext{
			Namespace: namespace}

		t.testContexts[namespace.Name] = testContext
	}
	return testContext
}

func (t *TestContextService) UpdateWithLease(namespace corev1.Namespace, lease v1.Lease) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	testContext := t.getTestContext(namespace)
	testContext.Pool = lease.Status.Name
	if len(lease.Spec.NetworkType) == 0 {
		lease.Spec.NetworkType = "multi-tenant"
	}
	testContext.NetworkType = string(lease.Spec.NetworkType)
	if len(lease.Status.Topology.Networks) > 0 {
		testContext.Portgroup = path.Base(lease.Status.Topology.Networks[0])
	}
}

func (t *TestContextService) UpdateWithPods(namespace corev1.Namespace, pod corev1.Pod) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	testContext := t.getTestContext(namespace)
	if pod.Status.Phase == corev1.PodFailed {
		testContext.Failed = true
		t.metricsContext.PodFailed(pod, testContext.Namespace.Labels["ci.openshift.io/metadata.target"], testContext.Namespace.Labels["ci.openshift.io/metadata.variant"])
	}
}

func (t *TestContextService) UpdateWithNamespace(namespace corev1.Namespace) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	testContext := t.getTestContext(namespace)
	testContext.Namespace = namespace
}

func (t *TestContextService) DestroyContext(namespace corev1.Namespace) *data.TestContext {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	testContext := t.getTestContext(namespace)
	outCtx := &data.TestContext{
		Pool:        testContext.Pool,
		Failed:      testContext.Failed,
		Namespace:   testContext.Namespace,
		NetworkType: testContext.NetworkType,
		Portgroup:   testContext.Portgroup,
	}
	delete(t.testContexts, namespace.Name)

	return outCtx
}

func (t *TestContextService) IsContextFailed(namespace corev1.Namespace) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	return t.getTestContext(namespace).Failed
}

func (t *TestContextService) GetPromLabelValues(testContext *data.TestContext) ([]string, error) {
	var promLabels []string
	var labelNames = []string{
		"ci.openshift.io/metadata.target",
		"ci.openshift.io/metadata.variant",
		"ci.openshift.io/jobtype",
	}

	labels := testContext.Namespace.Labels

	for _, labelName := range labelNames {
		if val, exists := labels[labelName]; exists {
			promLabels = append(promLabels, val)
		} else {
			promLabels = append(promLabels, "undefined")
		}
	}

	networkType, pool, portGroup := "multi-tenant", "undefined", "undefined"
	if len(testContext.NetworkType) > 0 {
		networkType = testContext.NetworkType
	}	

	if len(testContext.Pool) == 0 {
		return nil, fmt.Errorf("pool is empty")		
	}
	pool = testContext.Pool
	if len(testContext.Portgroup) == 0 {
		return nil, fmt.Errorf("port group is empty")		
	}
	portGroup = testContext.Portgroup
	return append(promLabels, pool, networkType, portGroup), nil
}

func (t *TestContextService) Pass(testContext *data.TestContext) {
	promLabels, err := t.GetPromLabelValues(testContext)
	if err != nil {
		t.log.Error(err, "error getting prom labels")	
		return
	}
	t.metricsContext.Pass(promLabels)
}

func (t *TestContextService) Fail(testContext *data.TestContext) {
	promLabels, err := t.GetPromLabelValues(testContext)
	if err != nil {
		t.log.Error(err, "error getting prom labels")
		return
	}
	t.metricsContext.Fail(promLabels)
}

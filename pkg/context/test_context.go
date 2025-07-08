package context

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sync"

	"github.com/go-logr/logr"
	"github.com/openshift-splat-team/test-monitor/pkg/data"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	corev1 "k8s.io/api/core/v1"
)

const testContextsFilename = "/context/test_contexts.json"

type TestContextService struct {
	testContexts   map[string]*data.TestContext
	metricsContext *MetricsContext
	mutex          *sync.Mutex
	log 			logr.Logger
}

func (t *TestContextService) Initialize(log logr.Logger) {
	t.log = log
	t.testContexts = make(map[string]*data.TestContext)
	t.mutex = &sync.Mutex{}
	err := t.Restore(testContextsFilename)
	if err != nil {
		log.Error(err, "error restoring test contexts")
	}	
	t.metricsContext = &MetricsContext{}
	t.metricsContext.Initialize()
}

// SaveTestContexts saves all test contexts to a file
func (t *TestContextService) Save(filename string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Create or overwrite the file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()

	// Encode the testContexts map to JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(t.testContexts); err != nil {
		return fmt.Errorf("failed to encode test contexts: %w", err)
	}

	t.log.Info("Successfully saved test contexts", "filename", filename, "count", len(t.testContexts))
	return nil
}

// RestoreTestContexts restores test contexts from a file
func (t *TestContextService) Restore(filename string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.log.Info("Restoring test contexts", "filename", filename)

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.log.Info("Test contexts file does not exist, starting with empty contexts", "filename", filename)
		return nil
	}

	// Open and read the file
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	// Decode the JSON into testContexts map
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&t.testContexts); err != nil {
		return fmt.Errorf("failed to decode test contexts: %w", err)
	}

	// Ensure the map is not nil
	if t.testContexts == nil {
		t.testContexts = make(map[string]*data.TestContext)
	}

	t.log.Info("Successfully restored test contexts", "filename", filename, "count", len(t.testContexts))
	return nil
}


// GetTestContextCount returns the number of active test contexts
func (t *TestContextService) GetTestContextCount() int {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return len(t.testContexts)
}

// GetTestContextSnapshot returns a copy of all test contexts for inspection
func (t *TestContextService) GetTestContextSnapshot() map[string]*data.TestContext {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	snapshot := make(map[string]*data.TestContext, len(t.testContexts))
	for key, value := range t.testContexts {
		// Create a copy of the test context
		snapshot[key] = &data.TestContext{
			Namespace:   value.Namespace,
			Failed:      value.Failed,
			Pool:        value.Pool,
			NetworkType: value.NetworkType,
			Portgroup:   value.Portgroup,
		}
	}
	return snapshot
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

	t.Save(testContextsFilename)	
	
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

package context

import (
	"sync"

	"github.com/openshift-splat-team/vsphere-capacity-manager/pkg/controller"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type MetricsContext struct {
	passCounter *prometheus.CounterVec
	failCounter *prometheus.CounterVec
	podCounter *prometheus.CounterVec
	mutex       *sync.Mutex
}

func (t *MetricsContext) Initialize() {
	t.passCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prow_ci_test_passes",
			Help: "The total number of passes for a given prow variant.",
		},
		[]string{"test_name", "variant", "job_type", "pool", "network_type", "vlan"},
	)
	
	t.failCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prow_ci_test_fails",
			Help: "The total number of fails for a given prow variant.",
		},
		[]string{"test_name", "variant", "job_type", "pool", "network_type", "vlan"},
	)

	t.podCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "prow_ci_pod_failures",
			Help: "The total number of pod failures for a given prow variant.",
		},
		[]string{"test_name", "variant", "pod_name", "node_name"},
	)

	t.mutex = &sync.Mutex{}

	metrics.Registry.MustRegister(t.passCounter, t.failCounter)	
	controller.InitMetrics()
}

// PodFailed increments the pod failure counter for a given pod and test name.
func (t *MetricsContext) PodFailed(pod corev1.Pod, testName string, variant string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()	
	promLabels := []string{testName, variant, pod.Name, pod.Spec.NodeName}
	t.podCounter.WithLabelValues(promLabels...).Add(1)
}

// Pass increments the pass counter for a given test name and variant.
func (t *MetricsContext) Pass(promLabels []string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()	
	t.passCounter.WithLabelValues(promLabels...).Add(1)
}

// Fail increments the fail counter for a given test name and variant.
func (t *MetricsContext) Fail(promLabels []string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.failCounter.WithLabelValues(promLabels...).Add(1)
}

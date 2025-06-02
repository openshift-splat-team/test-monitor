package context

import (
	"sync"

	"github.com/openshift-splat-team/vsphere-capacity-manager/pkg/controller"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type MetricsContext struct {
	passCounter *prometheus.CounterVec
	failCounter *prometheus.CounterVec
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

	t.mutex = &sync.Mutex{}

	metrics.Registry.MustRegister(t.passCounter, t.failCounter)
	controller.InitMetrics()
}

func (t *MetricsContext) Pass(promLabels []string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()	
	t.passCounter.WithLabelValues(promLabels...).Add(1)
}

func (t *MetricsContext) Fail(promLabels []string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.failCounter.WithLabelValues(promLabels...).Add(1)
}

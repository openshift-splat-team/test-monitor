package monitor

import "github.com/prometheus/client_golang/prometheus"

var (
	poolTestPasses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pool_test_passes",
		Help: "The status of tests for the pool",
	}, []string{"pool", "test", "portgroup"})

	poolTestFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pool_test_fails",
		Help: "The status of tests for the pool",
	}, []string{"pool", "test", "portgroup"})
)

type Monitor struct {
	failedNamespaces map[string]interface{}
}

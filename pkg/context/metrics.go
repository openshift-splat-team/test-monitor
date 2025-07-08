package context

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/openshift-splat-team/vsphere-capacity-manager/pkg/controller"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// MetricsSnapshot represents a snapshot of metrics that can be saved/restored
type MetricsSnapshot struct {
	PassCounters map[string]float64 `json:"pass_counters"`
	FailCounters map[string]float64 `json:"fail_counters"`
	PodCounters  map[string]float64 `json:"pod_counters"`
}

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

	metrics.Registry.MustRegister(t.passCounter, t.failCounter, t.podCounter)	
	controller.InitMetrics()
}

// SaveMetrics saves the current metrics to a file
func (t *MetricsContext) SaveMetrics(filename string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Create metrics snapshot
	snapshot := MetricsSnapshot{
		PassCounters: make(map[string]float64),
		FailCounters: make(map[string]float64),
		PodCounters:  make(map[string]float64),
	}

	// Gather current metrics
	metricFamilies, err := metrics.Registry.Gather()
	if err != nil {
		return fmt.Errorf("failed to gather metrics: %w", err)
	}

	// Extract counter values
	for _, mf := range metricFamilies {
		switch mf.GetName() {
		case "prow_ci_test_passes":
			snapshot.PassCounters = extractCounterValues(mf)
		case "prow_ci_test_fails":
			snapshot.FailCounters = extractCounterValues(mf)
		case "prow_ci_pod_failures":
			snapshot.PodCounters = extractCounterValues(mf)
		}
	}

	// Save to file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("failed to encode metrics: %w", err)
	}

	return nil
}

// RestoreMetrics restores metrics from a file
func (t *MetricsContext) RestoreMetrics(filename string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Read file
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer file.Close()

	// Decode snapshot
	var snapshot MetricsSnapshot
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("failed to decode metrics: %w", err)
	}

	// Restore counter values
	if err := t.restoreCounterValues(t.passCounter, snapshot.PassCounters); err != nil {
		return fmt.Errorf("failed to restore pass counters: %w", err)
	}
	if err := t.restoreCounterValues(t.failCounter, snapshot.FailCounters); err != nil {
		return fmt.Errorf("failed to restore fail counters: %w", err)
	}
	if err := t.restoreCounterValues(t.podCounter, snapshot.PodCounters); err != nil {
		return fmt.Errorf("failed to restore pod counters: %w", err)
	}

	return nil
}

// extractCounterValues extracts counter values from a metric family
func extractCounterValues(mf *dto.MetricFamily) map[string]float64 {
	values := make(map[string]float64)
	for _, metric := range mf.GetMetric() {
		// Create a key from label values
		key := ""
		for i, label := range metric.GetLabel() {
			if i > 0 {
				key += ","
			}
			key += label.GetValue()
		}
		values[key] = metric.GetCounter().GetValue()
	}
	return values
}

// restoreCounterValues restores counter values to a CounterVec
func (t *MetricsContext) restoreCounterValues(counterVec *prometheus.CounterVec, values map[string]float64) error {
	for key, value := range values {
		// Parse the key back to label values
		labels := parseLabels(key)
		// Add the difference to reach the target value
		counter := counterVec.WithLabelValues(labels...)
		counter.Add(value)
	}
	return nil
}

// parseLabels parses comma-separated labels back to a slice
func parseLabels(key string) []string {
	if key == "" {
		return []string{}
	}
	// Simple comma split - you might want to make this more robust
	// depending on your label values
	labels := []string{}
	current := ""
	for _, char := range key {
		if char == ',' {
			labels = append(labels, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		labels = append(labels, current)
	}
	return labels
}

// SaveMetricsPrometheusFormat saves metrics in Prometheus text format
func (t *MetricsContext) SaveMetricsPrometheusFormat(filename string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Gather metrics
	metricFamilies, err := metrics.Registry.Gather()
	if err != nil {
		return fmt.Errorf("failed to gather metrics: %w", err)
	}

	// Create file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}
	defer file.Close()

	// Write metrics in Prometheus text format
	encoder := expfmt.NewEncoder(file, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range metricFamilies {
		if err := encoder.Encode(mf); err != nil {
			return fmt.Errorf("failed to encode metric family: %w", err)
		}
	}

	return nil
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

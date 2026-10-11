package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	Registry       *prometheus.Registry
	Counters       map[string]prometheus.Counter
	RunDuration    prometheus.Histogram
	OllamaDuration prometheus.Histogram
	Review         prometheus.Gauge
}

func New() *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry(), Counters: map[string]prometheus.Counter{}}
	for _, name := range []string{"collector_runs_total", "collector_mentions_discovered_total", "collector_mentions_stored_total", "collector_duplicates_total", "collector_errors_total", "collector_human_action_required_total", "browser_actions_total", "browser_action_errors_total", "agent_steps_total", "agent_rejected_actions_total", "ollama_requests_total", "analysis_results_total", "cases_total"} {
		c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: name})
		m.Counters[name] = c
		m.Registry.MustRegister(c)
	}
	m.RunDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "collector_runs_duration_seconds", Help: "Collection run duration", Buckets: prometheus.ExponentialBuckets(1, 4, 7)})
	m.OllamaDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "ollama_request_duration_seconds", Help: "Local inference duration", Buckets: prometheus.ExponentialBuckets(0.1, 4, 7)})
	m.Review = prometheus.NewGauge(prometheus.GaugeOpts{Name: "review_queue_size", Help: "Mentions requiring review"})
	m.Registry.MustRegister(m.RunDuration, m.OllamaDuration, m.Review)
	return m
}
func (m *Metrics) Inc(name string) {
	if m != nil {
		m.Counters[name].Inc()
	}
}

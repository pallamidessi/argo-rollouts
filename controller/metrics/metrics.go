package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	registry "k8s.io/component-base/metrics/legacyregistry"

	// make sure to register workqueue prometheus metrics
	_ "k8s.io/component-base/metrics/prometheus/workqueue"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/log"
)

type MetricsServer struct {
	*http.Server
	reconcileRolloutHistogram *prometheus.HistogramVec
	errorRolloutCounter       *prometheus.CounterVec

	reconcileExperimentHistogram *prometheus.HistogramVec
	errorExperimentCounter       *prometheus.CounterVec

	reconcileAnalysisRunHistogram *prometheus.HistogramVec
	errorAnalysisRunCounter       *prometheus.CounterVec

	k8sRequestsCounter *K8sRequestsCountProvider
}

const (
	// MetricsPath is the endpoint to collect rollout metrics
	MetricsPath = "/metrics"
)

type ServerConfig struct {
	Addr                          string
	RolloutLister                 rolloutlister.RolloutLister
	AnalysisRunLister             rolloutlister.AnalysisRunLister
	AnalysisTemplateLister        rolloutlister.AnalysisTemplateLister
	ClusterAnalysisTemplateLister rolloutlister.ClusterAnalysisTemplateLister
	ExperimentLister              rolloutlister.ExperimentLister
	K8SRequestProvider            *K8sRequestsCountProvider
}

// NewMetricsServer returns a new prometheus server which collects rollout metrics
func NewMetricsServer(cfg ServerConfig, isPrimary bool) *MetricsServer {
	mux := http.NewServeMux()

	reg := prometheus.NewRegistry()

	// secondary controller doesn't expose any metrics
	if !isPrimary {
		mux.Handle(MetricsPath, promhttp.HandlerFor(prometheus.Gatherers{
			reg,
		}, promhttp.HandlerOpts{}))
		return &MetricsServer{
			Server: &http.Server{
				Addr:    cfg.Addr,
				Handler: mux,
			},
		}
	}

	reg.MustRegister(NewRolloutCollector(cfg.RolloutLister))
	reg.MustRegister(NewAnalysisRunCollector(cfg.AnalysisRunLister, cfg.AnalysisTemplateLister, cfg.ClusterAnalysisTemplateLister))
	reg.MustRegister(NewExperimentCollector(cfg.ExperimentLister))
	cfg.K8SRequestProvider.MustRegister(reg)
	reg.MustRegister(MetricRolloutReconcile)
	reg.MustRegister(MetricRolloutReconcileError)
	reg.MustRegister(MetricRolloutEventsTotal)
	reg.MustRegister(MetricExperimentReconcile)
	reg.MustRegister(MetricExperimentReconcileError)
	reg.MustRegister(MetricAnalysisRunReconcile)
	reg.MustRegister(MetricAnalysisRunReconcileError)

	mux.Handle(MetricsPath, promhttp.HandlerFor(prometheus.Gatherers{
		// contains app controller specific metrics
		reg,
		// contains process, golang and controller workqueues metrics
		registry.DefaultGatherer,
	}, promhttp.HandlerOpts{}))
	return &MetricsServer{
		Server: &http.Server{
			Addr:    cfg.Addr,
			Handler: mux,
		},
		reconcileRolloutHistogram: MetricRolloutReconcile,
		errorRolloutCounter:       MetricRolloutReconcileError,

		reconcileExperimentHistogram: MetricExperimentReconcile,
		errorExperimentCounter:       MetricExperimentReconcileError,

		reconcileAnalysisRunHistogram: MetricAnalysisRunReconcile,
		errorAnalysisRunCounter:       MetricAnalysisRunReconcileError,

		k8sRequestsCounter: cfg.K8SRequestProvider,
	}
}

// IncRolloutReconcile increments the reconcile counter for a Rollout
func (m *MetricsServer) IncRolloutReconcile(rollout *v1alpha1.Rollout, duration time.Duration) {
	m.reconcileRolloutHistogram.WithLabelValues(rollout.Namespace, rollout.Name).Observe(duration.Seconds())
}

// IncExperimentReconcile increments the reconcile counter for an Experiment
func (m *MetricsServer) IncExperimentReconcile(ex *v1alpha1.Experiment, duration time.Duration) {
	m.reconcileExperimentHistogram.WithLabelValues(ex.Namespace, ex.Name).Observe(duration.Seconds())
}

// IncAnalysisRunReconcile increments the reconcile counter for an AnalysisRun
func (m *MetricsServer) IncAnalysisRunReconcile(ar *v1alpha1.AnalysisRun, duration time.Duration) {
	m.reconcileAnalysisRunHistogram.WithLabelValues(ar.Namespace, ar.Name).Observe(duration.Seconds())
}

// IncError increments the reconcile counter for an rollout
func (m *MetricsServer) IncError(namespace, name string, kind string) {
	switch kind {
	case log.RolloutKey:
		m.errorRolloutCounter.WithLabelValues(namespace, name).Inc()
	case log.AnalysisRunKey:
		m.errorAnalysisRunCounter.WithLabelValues(namespace, name).Inc()
	case log.ExperimentKey:
		m.errorExperimentCounter.WithLabelValues(namespace, name).Inc()
	}
}

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

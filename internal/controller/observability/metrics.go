// Package metrics defines this operator's domain-specific Prometheus
// metrics -- distinct from controller-runtime's own built-in metrics
// (reconcile counts, workqueue depth, REST client calls), which already
// cover generic controller health and need no duplication here.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// All metrics here register into controller-runtime's own metrics.Registry,
// so they're exposed on the same /metrics endpoint the operator already
// serves (see cmd/main.go's metricsServerOptions) -- no separate server,
// port, or scrape config needed.
//
// Outcome label values are deliberately a small, fixed set (see
// errorclass.go's outcome* constants), never raw error strings or SDK error
// codes: Prometheus labels are meant to stay low-cardinality, and unbounded
// values from arbitrary error text would make these both a cardinality risk
// and impossible to write a stable alert against.
var (
	// StorageReconcileTotal counts every storage reconcile attempt (bucket +
	// IRSA/access key provisioning), by provider and outcome. Answers "is
	// storage provisioning actually working, and for which provider" --
	// alert on a sustained rate of anything other than outcome="ready".
	StorageReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forge_storage_reconcile_total",
			Help: "Total storage reconcile attempts, by provider and outcome (ready, not_owned, timeout, access_denied, other_error).",
		},
		[]string{"provider", "outcome"},
	)

	// StorageReconcileDuration times the cloud-call-bound portion of a
	// storage reconcile. Bucketed out to storageReconcileTimeout (90s) so a
	// p99 creeping toward the ceiling is visible as an early warning, before
	// reconciles actually start timing out.
	StorageReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "forge_storage_reconcile_duration_seconds",
			Help:    "Time spent reconciling storage (bucket + IRSA/access key), by provider.",
			Buckets: []float64{0.5, 1, 2.5, 5, 10, 20, 30, 45, 60, 75, 90},
		},
		[]string{"provider"},
	)

	// StorageReady reflects the current StorageReady condition for a single
	// Application -- 1 when its last reconcile succeeded, 0 otherwise. One
	// time series per Application that has spec.storage set (not per
	// reconcile volume), so cardinality scales with fleet size, not
	// traffic -- fine at any realistic scale for this operator, but the
	// reason this is the one metric here with a cardinality shape worth
	// knowing about. Series are deleted when an Application finishes
	// finalizing (see finalizer.go) so this never accumulates stale entries
	// for Applications that no longer exist.
	StorageReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "forge_storage_ready",
			Help: "Whether an Application's storage currently reports Ready (1) or not (0), by namespace, name, and provider.",
		},
		[]string{"namespace", "name", "provider"},
	)

	// StorageBucketAdoptedTotal counts bucket ownership adoptions via the
	// adopt-bucket annotation specifically -- not the ordinary "we created
	// this bucket ourselves" recovery path. Each increment means this
	// operator just took over a bucket a *different* Application previously
	// owned: a deliberate, rare, security-relevant action that belongs on
	// its own audit signal, not folded into a generic success counter.
	StorageBucketAdoptedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forge_storage_bucket_adopted_total",
			Help: "Bucket ownership adoptions via the adopt-bucket annotation, by provider.",
		},
		[]string{"provider"},
	)

	// FinalizerCleanupTotal counts every storage cleanup attempt during
	// finalization, by provider and outcome. Answers "are deletions getting
	// stuck" -- outcome="timeout" means finalizerCleanupTimeout was hit and
	// an Application can't be deleted without someone going to look at that
	// specific bucket.
	FinalizerCleanupTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forge_finalizer_cleanup_total",
			Help: "Total storage cleanup attempts during finalization, by provider and outcome (success, timeout, access_denied, other_error).",
		},
		[]string{"provider", "outcome"},
	)

	// FinalizerCleanupDuration times storage cleanup during finalization.
	// Bucketed out to finalizerCleanupTimeout (5m), for the same
	// early-warning reason as StorageReconcileDuration.
	FinalizerCleanupDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "forge_finalizer_cleanup_duration_seconds",
			Help:    "Time spent cleaning up storage during finalization, by provider.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 180, 240, 270, 300},
		},
		[]string{"provider"},
	)

	// ApplicationReady reflects the current overall Ready condition for a
	// single Application -- 1 when its last reconcile settled it Ready, 0
	// otherwise. Distinct from StorageReady: this covers the whole
	// Application (Deployment/Service/ConfigMap/Ingress/HPA/PDB/Secret, not
	// just storage), answering "which Applications are unhealthy right now"
	// fleet-wide -- a question no existing metric (ours or
	// controller-runtime's own generic per-controller counters) can answer,
	// since those only give rates of past reconcile attempts, not current
	// state. Same cardinality shape and same series-deletion-on-finalize
	// discipline as StorageReady.
	ApplicationReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "forge_application_ready",
			Help: "Whether an Application currently reports the overall Ready condition (1) or not (0), by namespace and name.",
		},
		[]string{"namespace", "name"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		StorageReconcileTotal,
		StorageReconcileDuration,
		StorageReady,
		StorageBucketAdoptedTotal,
		FinalizerCleanupTotal,
		FinalizerCleanupDuration,
		ApplicationReady,
	)
}

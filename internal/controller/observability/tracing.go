// Tracing (this file) sets up OpenTelemetry distributed tracing for this
// operator, alongside the Prometheus metrics in metrics.go -- both live in
// this one observability package deliberately, as the shared home for this
// operator's telemetry (room for e.g. logging helpers later too, same
// reasoning as the package's own name). Off by default: unless Init is
// called (see cmd/main.go, gated on OTEL_EXPORTER_OTLP_ENDPOINT being set),
// every span created via Tracer().Start becomes a no-op -- a property of
// the OpenTelemetry API itself, not something this package implements -- so
// a deployment with nothing running to receive traces pays no cost and
// makes no network calls. Business logic never needs an "is tracing
// enabled" check.
package observability

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies this operator as the source of every span it
// creates -- shows up as the "instrumentation scope" in Jaeger/Tempo/etc.,
// and doubles as the OTel service name resource attribute.
const tracerName = "forge-operator"

// Tracer returns this operator's tracer. Safe to call at any point,
// including before Init or when Init is never called at all -- see the
// package doc comment.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Init configures the global TracerProvider to export spans via OTLP/gRPC.
// The exporter reads its actual target from the standard
// OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT)
// environment variable itself -- Init doesn't take an endpoint parameter or
// re-check that the variable is set; the caller (cmd/main.go) decides
// *whether* to call Init at all.
//
// Returns a shutdown func that flushes any buffered spans and closes the
// exporter. Callers must call it before process exit (with a fresh,
// non-cancelled context -- the shutdown flush needs its own brief window to
// actually send anything, separate from whatever context triggered
// shutdown in the first place) or spans from the final moments before
// shutdown can be silently lost.
func Init(ctx context.Context) (shutdown func(context.Context) error, err error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	resourceOpts := []resource.Option{
		// Distinguishes this operator's own deployment(s) from any other
		// service sharing the same Jaeger/Tempo/etc. backend.
		resource.WithAttributes(semconv.ServiceName(tracerName)),
		// service.instance.id: HOSTNAME is set by Kubernetes to the pod's
		// own name in every pod, no Downward API wiring needed -- lets
		// traces from different replicas (or, over time, the same replica
		// after a restart) be told apart.
		resource.WithAttributes(semconv.ServiceInstanceID(os.Getenv("HOSTNAME"))),
		// OTEL_RESOURCE_ATTRIBUTES is the standard OTel mechanism for
		// attaching whatever this specific deployment wants every span to
		// carry -- e.g. deployment.environment.name=production or
		// k8s.cluster.name=my-cluster -- neither of which is reliably
		// auto-detectable, so this is left to the deployer rather than
		// guessed at. Same "use the standard env var, don't invent a flag"
		// choice as OTEL_EXPORTER_OTLP_ENDPOINT above.
		resource.WithFromEnv(),
	}

	// k8s.namespace.name: which namespace *this operator* runs in (not the
	// Application being reconciled -- a different concept). Read from the
	// namespace file every pod already has mounted via its ServiceAccount
	// token projection, rather than requiring a Downward API env var in the
	// Helm chart. Best-effort: outside a real cluster (e.g. running the
	// binary locally) this file won't exist, which is fine -- the resource
	// is simply missing this one attribute, not an error.
	if data, readErr := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); readErr == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			resourceOpts = append(resourceOpts, resource.WithAttributes(semconv.K8SNamespaceName(ns)))
		}
	}

	res, err := resource.New(ctx, resourceOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenTelemetry resource: %w", err)
	}

	// AlwaysSample is the SDK's own default (see sdktrace.NewTracerProvider's
	// doc comment) and is fine to leave as-is: this operator's trace volume
	// -- one per Application reconcile, not per incoming web request -- is
	// nowhere near what sampling knobs exist to protect against. Revisit if
	// that stops being true.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

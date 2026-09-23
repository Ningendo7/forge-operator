// Tracing (this file) sets up OpenTelemetry distributed tracing for this
// operator, alongside the Prometheus metrics in metrics.go. Off by default:
// unless Init is called (see cmd/main.go, gated on
// OTEL_EXPORTER_OTLP_ENDPOINT), every span from Tracer().Start is a no-op --
// an OpenTelemetry API property, not something this package implements --
// so callers never need an "is tracing enabled" check.
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

// serviceAccountNamespaceFile is a var, not an inline literal, so tests can
// point it at a temp file and exercise both the present and missing cases
// deterministically.
var serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Tracer returns this operator's tracer. Safe to call before Init, or if
// Init is never called at all -- see the package doc comment.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Init configures the global TracerProvider to export spans via OTLP/gRPC,
// reading its target from the standard OTEL_EXPORTER_OTLP_ENDPOINT env var.
// Returns a shutdown func that flushes buffered spans and closes the
// exporter -- call it with a fresh, non-cancelled context before process
// exit, or spans from the final moments can be lost.
func Init(ctx context.Context) (shutdown func(context.Context) error, err error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx, buildResourceOptions()...)
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenTelemetry resource: %w", err)
	}

	// AlwaysSample (the SDK default): fine at this trace volume -- one per
	// Application reconcile, not per incoming request.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

// buildResourceOptions is factored out of Init so its one piece of real
// conditional logic (the best-effort namespace file read below) is
// unit-testable without a real OTLP exporter or global tracer state.
func buildResourceOptions() []resource.Option {
	resourceOpts := []resource.Option{
		resource.WithAttributes(semconv.ServiceName(tracerName)),
		// HOSTNAME is set by Kubernetes to the pod's own name -- tells
		// replicas apart without Downward API wiring.
		resource.WithAttributes(semconv.ServiceInstanceID(os.Getenv("HOSTNAME"))),
		// Standard OTel mechanism for deployment-specific attributes
		// (deployment.environment.name, k8s.cluster.name, ...) that aren't
		// reliably auto-detectable.
		resource.WithFromEnv(),
	}

	// k8s.namespace.name: this operator's own namespace (not the
	// Application being reconciled). Read from the ServiceAccount token
	// projection every pod already has, rather than a Downward API env
	// var. Best-effort: missing outside a real cluster, which is fine.
	if data, readErr := os.ReadFile(serviceAccountNamespaceFile); readErr == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			resourceOpts = append(resourceOpts, resource.WithAttributes(semconv.K8SNamespaceName(ns)))
		}
	}

	return resourceOpts
}

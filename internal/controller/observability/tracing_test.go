package observability

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// --- buildResourceOptions ---

func withServiceAccountNamespaceFile(t *testing.T, path string) {
	t.Helper()
	original := serviceAccountNamespaceFile
	serviceAccountNamespaceFile = path
	t.Cleanup(func() { serviceAccountNamespaceFile = original })
}

func TestBuildResourceOptions_SkipsNamespaceAttributeWhenFileMissing(t *testing.T) {
	withServiceAccountNamespaceFile(t, filepath.Join(t.TempDir(), "does-not-exist"))

	res, err := resource.New(context.Background(), buildResourceOptions()...)
	if err != nil {
		t.Fatalf("resource.New returned error: %v", err)
	}

	if _, ok := res.Set().Value(semconv.K8SNamespaceNameKey); ok {
		t.Fatalf("expected no k8s.namespace.name attribute when the namespace file is missing")
	}
}

func TestBuildResourceOptions_SkipsNamespaceAttributeWhenFileEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "namespace")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("failed to write test namespace file: %v", err)
	}
	withServiceAccountNamespaceFile(t, path)

	res, err := resource.New(context.Background(), buildResourceOptions()...)
	if err != nil {
		t.Fatalf("resource.New returned error: %v", err)
	}

	if _, ok := res.Set().Value(semconv.K8SNamespaceNameKey); ok {
		t.Fatalf("expected no k8s.namespace.name attribute when the namespace file is blank")
	}
}

func TestBuildResourceOptions_SetsNamespaceAttributeWhenFilePresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "namespace")
	if err := os.WriteFile(path, []byte("forge-system\n"), 0o600); err != nil {
		t.Fatalf("failed to write test namespace file: %v", err)
	}
	withServiceAccountNamespaceFile(t, path)

	res, err := resource.New(context.Background(), buildResourceOptions()...)
	if err != nil {
		t.Fatalf("resource.New returned error: %v", err)
	}

	got, ok := res.Set().Value(semconv.K8SNamespaceNameKey)
	if !ok {
		t.Fatalf("expected a k8s.namespace.name attribute when the namespace file is present")
	}
	if got.AsString() != "forge-system" {
		t.Fatalf("expected k8s.namespace.name %q, got %q", "forge-system", got.AsString())
	}
}

func TestBuildResourceOptions_AlwaysSetsServiceName(t *testing.T) {
	withServiceAccountNamespaceFile(t, filepath.Join(t.TempDir(), "does-not-exist"))

	res, err := resource.New(context.Background(), buildResourceOptions()...)
	if err != nil {
		t.Fatalf("resource.New returned error: %v", err)
	}

	got, ok := res.Set().Value(semconv.ServiceNameKey)
	if !ok {
		t.Fatalf("expected a service.name attribute")
	}
	if got.AsString() != tracerName {
		t.Fatalf("expected service.name %q, got %q", tracerName, got.AsString())
	}
}

// --- Tracer ---

func TestTracer_UsableBeforeInit(t *testing.T) {
	// The package doc comment's central promise: every span created via
	// Tracer().Start is a documented no-op until Init runs, never a panic
	// or nil dereference -- business logic elsewhere in this operator
	// never needs an "is tracing enabled" check.
	_, span := Tracer().Start(context.Background(), "test-span")
	defer span.End()

	if span == nil {
		t.Fatalf("expected a non-nil (no-op) span before Init")
	}
}

// --- Init ---

func TestInit_SucceedsWithoutReachableCollector(t *testing.T) {
	// Init itself never checks whether OTEL_EXPORTER_OTLP_ENDPOINT is set
	// or reachable -- see the package doc comment, that decision belongs
	// to cmd/main.go. Constructing the exporter and TracerProvider must
	// succeed regardless, since the gRPC client dials lazily rather than
	// blocking here.
	originalTP := otel.GetTracerProvider()
	originalPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(originalTP)
		otel.SetTextMapPropagator(originalPropagator)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, err := Init(ctx)
	if err != nil {
		t.Fatalf("Init returned error with no reachable collector: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected a non-nil shutdown func")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestInit_SetsGlobalTracerProviderAndPropagator(t *testing.T) {
	originalTP := otel.GetTracerProvider()
	originalPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(originalTP)
		otel.SetTextMapPropagator(originalPropagator)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shutdown, err := Init(ctx)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = shutdown(shutdownCtx)
	}()

	if otel.GetTracerProvider() == originalTP {
		t.Fatalf("expected Init to install a new global TracerProvider")
	}
	if _, ok := otel.GetTextMapPropagator().(propagation.TraceContext); !ok {
		t.Fatalf("expected Init to install a propagation.TraceContext propagator, got %T", otel.GetTextMapPropagator())
	}
}

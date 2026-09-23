/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"testing"

	smithymiddleware "github.com/aws/smithy-go/middleware"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"golang.org/x/time/rate"

	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
)

// --- NewLimiter ---

func TestNewLimiter_FloorsNonPositiveQPS(t *testing.T) {
	lim := NewLimiter(0, 10)
	if lim.Limit() != rate.Limit(1) {
		t.Fatalf("expected QPS to floor to 1, got %v", lim.Limit())
	}

	lim = NewLimiter(-5, 10)
	if lim.Limit() != rate.Limit(1) {
		t.Fatalf("expected negative QPS to floor to 1, got %v", lim.Limit())
	}
}

func TestNewLimiter_FloorsNonPositiveBurst(t *testing.T) {
	lim := NewLimiter(10, 0)
	if lim.Burst() != 1 {
		t.Fatalf("expected burst to floor to 1, got %v", lim.Burst())
	}

	lim = NewLimiter(10, -3)
	if lim.Burst() != 1 {
		t.Fatalf("expected negative burst to floor to 1, got %v", lim.Burst())
	}
}

func TestNewLimiter_UsesProvidedValues(t *testing.T) {
	lim := NewLimiter(20, 40)
	if lim.Limit() != rate.Limit(20) {
		t.Fatalf("expected QPS 20, got %v", lim.Limit())
	}
	if lim.Burst() != 40 {
		t.Fatalf("expected burst 40, got %v", lim.Burst())
	}
}

// histogramSampleCount returns how many observations
// forge_rate_limit_wait_duration_seconds has recorded under the given
// "limiter" label so far.
func histogramSampleCount(t *testing.T, label string) uint64 {
	t.Helper()
	hist, ok := forgemetrics.RateLimitWaitDuration.WithLabelValues(label).(prometheus.Histogram)
	if !ok {
		t.Fatalf("expected a prometheus.Histogram for label %q", label)
	}
	var m dto.Metric
	if err := hist.Write(&m); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}

// --- AWSMiddleware ---

// fakeFinalizeHandler is the next handler AWSMiddleware delegates to;
// tests assert on whether it was reached to distinguish "waited then
// proceeded" from "failed before proceeding".
type fakeFinalizeHandler struct {
	called bool
}

func (f *fakeFinalizeHandler) HandleFinalize(ctx context.Context, in smithymiddleware.FinalizeInput) (
	smithymiddleware.FinalizeOutput, smithymiddleware.Metadata, error,
) {
	f.called = true
	return smithymiddleware.FinalizeOutput{}, smithymiddleware.Metadata{}, nil
}

// awsRateLimitMiddleware builds a Stack, applies AWSMiddleware(limiter,
// name) to it exactly as s3/client.go and akamaiobjstr/client.go do via
// Options.APIOptions, and returns the registered "RateLimit" middleware so
// tests can invoke it directly without standing up a real AWS client.
func awsRateLimitMiddleware(t *testing.T, limiter *rate.Limiter, name string) smithymiddleware.FinalizeMiddleware {
	t.Helper()
	stack := smithymiddleware.NewStack("test", func() any { return struct{}{} })
	if err := AWSMiddleware(limiter, name)(stack); err != nil {
		t.Fatalf("AWSMiddleware returned error: %v", err)
	}
	mw, ok := stack.Finalize.Get("RateLimit")
	if !ok {
		t.Fatalf("expected a %q middleware to be registered in the Finalize step", "RateLimit")
	}
	return mw
}

func TestAWSMiddleware_WaitsThenProceeds(t *testing.T) {
	mw := awsRateLimitMiddleware(t, rate.NewLimiter(rate.Inf, 1), "test-waits-then-proceeds")
	next := &fakeFinalizeHandler{}

	_, _, err := mw.HandleFinalize(context.Background(), smithymiddleware.FinalizeInput{}, next)
	if err != nil {
		t.Fatalf("HandleFinalize returned error: %v", err)
	}
	if !next.called {
		t.Fatalf("expected the next handler to be reached once a token was available")
	}
}

func TestAWSMiddleware_ContextCancelledStopsBeforeNext(t *testing.T) {
	// A limiter with plenty of capacity: proves the failure comes from ctx
	// cancellation, not from the limiter having no tokens to give.
	mw := awsRateLimitMiddleware(t, rate.NewLimiter(rate.Inf, 1), "test-ctx-cancelled")
	next := &fakeFinalizeHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := mw.HandleFinalize(ctx, smithymiddleware.FinalizeInput{}, next)
	if err == nil {
		t.Fatalf("expected an error when ctx is already cancelled, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to wrap context.Canceled, got %v", err)
	}
	if next.called {
		t.Fatalf("expected the next handler NOT to be reached when the wait fails")
	}
}

func TestAWSMiddleware_NilLimiterIsNoOp(t *testing.T) {
	mw := awsRateLimitMiddleware(t, nil, "test-nil-limiter")
	next := &fakeFinalizeHandler{}

	// An already-cancelled ctx would fail Wait on any real limiter (see
	// TestAWSMiddleware_ContextCancelledStopsBeforeNext) -- proceeding
	// anyway here proves Wait was skipped entirely, not merely that it
	// returned quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := mw.HandleFinalize(ctx, smithymiddleware.FinalizeInput{}, next)
	if err != nil {
		t.Fatalf("expected a nil limiter to be a no-op, got error: %v", err)
	}
	if !next.called {
		t.Fatalf("expected the next handler to be reached when limiter is nil")
	}
}

func TestAWSMiddleware_ObservesWaitDurationUnderName(t *testing.T) {
	const label = "test-aws-observes-wait-duration"
	forgemetrics.RateLimitWaitDuration.Reset()

	mw := awsRateLimitMiddleware(t, rate.NewLimiter(rate.Inf, 1), label)
	if _, _, err := mw.HandleFinalize(context.Background(), smithymiddleware.FinalizeInput{}, &fakeFinalizeHandler{}); err != nil {
		t.Fatalf("HandleFinalize returned error: %v", err)
	}

	if got := histogramSampleCount(t, label); got != 1 {
		t.Fatalf("expected 1 observation under label %q, got %d", label, got)
	}
}

func TestAWSMiddleware_ObservesWaitDurationEvenOnFailure(t *testing.T) {
	// A failed wait (ctx cancelled, burst exhausted, etc.) is exactly the
	// case operators most want visibility into -- it must still be
	// recorded, not skipped because the call never proceeded.
	const label = "test-aws-observes-on-failure"
	forgemetrics.RateLimitWaitDuration.Reset()

	mw := awsRateLimitMiddleware(t, rate.NewLimiter(rate.Inf, 1), label)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := mw.HandleFinalize(ctx, smithymiddleware.FinalizeInput{}, &fakeFinalizeHandler{}); err == nil {
		t.Fatalf("expected an error from the cancelled ctx")
	}

	if got := histogramSampleCount(t, label); got != 1 {
		t.Fatalf("expected 1 observation under label %q even though the wait failed, got %d", label, got)
	}
}

func TestAWSMiddleware_NilLimiterDoesNotObserve(t *testing.T) {
	const label = "test-aws-nil-limiter-no-observe"
	forgemetrics.RateLimitWaitDuration.Reset()

	mw := awsRateLimitMiddleware(t, nil, label)
	if _, _, err := mw.HandleFinalize(context.Background(), smithymiddleware.FinalizeInput{}, &fakeFinalizeHandler{}); err != nil {
		t.Fatalf("HandleFinalize returned error: %v", err)
	}

	if got := histogramSampleCount(t, label); got != 0 {
		t.Fatalf("expected no observation when limiter is nil, got %d", got)
	}
}

// --- RoundTripper ---

// fakeRoundTripper is the next transport NewRoundTripper delegates to;
// tests assert on whether it was reached the same way fakeFinalizeHandler
// does for AWSMiddleware.
type fakeRoundTripper struct {
	called bool
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.called = true
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func newTestRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("failed to build test request: %v", err)
	}
	return req
}

func TestRoundTripper_WaitsThenProceeds(t *testing.T) {
	next := &fakeRoundTripper{}
	rt := NewRoundTripper(rate.NewLimiter(rate.Inf, 1), next, "test-rt-waits-then-proceeds")

	if _, err := rt.RoundTrip(newTestRequest(t, context.Background())); err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if !next.called {
		t.Fatalf("expected the next RoundTripper to be reached once a token was available")
	}
}

func TestRoundTripper_ContextCancelledStopsBeforeNext(t *testing.T) {
	next := &fakeRoundTripper{}
	rt := NewRoundTripper(rate.NewLimiter(rate.Inf, 1), next, "test-rt-ctx-cancelled")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := rt.RoundTrip(newTestRequest(t, ctx))
	if err == nil {
		t.Fatalf("expected an error when ctx is already cancelled, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to wrap context.Canceled, got %v", err)
	}
	if next.called {
		t.Fatalf("expected the next RoundTripper NOT to be reached when the wait fails")
	}
}

func TestRoundTripper_NilLimiterIsNoOp(t *testing.T) {
	next := &fakeRoundTripper{}
	rt := NewRoundTripper(nil, next, "test-rt-nil-limiter")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rt.RoundTrip(newTestRequest(t, ctx)); err != nil {
		t.Fatalf("expected a nil limiter to be a no-op, got error: %v", err)
	}
	if !next.called {
		t.Fatalf("expected the next RoundTripper to be reached when limiter is nil")
	}
}

func TestNewRoundTripper_NilNextDefaultsToDefaultTransport(t *testing.T) {
	rt := NewRoundTripper(rate.NewLimiter(rate.Inf, 1), nil, "test-rt-nil-next")
	if rt.next != http.DefaultTransport {
		t.Fatalf("expected a nil next to default to http.DefaultTransport, got %#v", rt.next)
	}
}

func TestRoundTripper_ObservesWaitDurationUnderName(t *testing.T) {
	const label = "test-rt-observes-wait-duration"
	forgemetrics.RateLimitWaitDuration.Reset()

	rt := NewRoundTripper(rate.NewLimiter(rate.Inf, 1), &fakeRoundTripper{}, label)
	if _, err := rt.RoundTrip(newTestRequest(t, context.Background())); err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}

	if got := histogramSampleCount(t, label); got != 1 {
		t.Fatalf("expected 1 observation under label %q, got %d", label, got)
	}
}

func TestRoundTripper_NilLimiterDoesNotObserve(t *testing.T) {
	const label = "test-rt-nil-limiter-no-observe"
	forgemetrics.RateLimitWaitDuration.Reset()

	rt := NewRoundTripper(nil, &fakeRoundTripper{}, label)
	if _, err := rt.RoundTrip(newTestRequest(t, context.Background())); err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}

	if got := histogramSampleCount(t, label); got != 0 {
		t.Fatalf("expected no observation when limiter is nil, got %d", got)
	}
}

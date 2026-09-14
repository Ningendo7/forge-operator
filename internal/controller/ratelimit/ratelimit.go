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

// Package ratelimit paces this operator's own outgoing calls to AWS and
// Akamai/Linode, independent of MaxConcurrentReconciles -- that bounds how
// many reconciles run in parallel, not how many external API calls they
// collectively make per second. At small scale the difference is
// invisible; at fleet scale (a mass resync after a restart, a cluster-wide
// spec change touching every Application at once) 5 concurrent workers
// each making a handful of sequential AWS/Akamai calls can genuinely burst
// past a provider's real rate limits, especially AWS IAM's, which are far
// tighter than S3's.
//
// Each external surface gets its own independent limiter rather than one
// shared budget -- S3, IAM, Akamai's account API, and Akamai's
// S3-compatible object endpoint are four genuinely different pieces of
// infrastructure with different real capacities. Sharing one budget across
// any of them means the tightest one throttles the others down to its own
// ceiling for no reason tied to their actual capacity.
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	smithymiddleware "github.com/aws/smithy-go/middleware"
	"golang.org/x/time/rate"

	forgemetrics "github.com/Ningendo7/forge-operator/internal/controller/observability"
)

// NewLimiter builds a token-bucket limiter from a QPS/burst pair. Values
// <= 0 fall back to 1 QPS / burst of 1 -- a last-resort floor, not a
// recommended value, so a misconfigured input never produces a limiter
// that blocks forever (rate.Limit(0)) or rejects every request outright
// (burst 0). Callers should pass a real, provider-appropriate default from
// their own configuration layer (see cmd/main.go's envFloat/envInt)
// instead of relying on this floor.
func NewLimiter(qps float64, burst int) *rate.Limiter {
	if qps <= 0 {
		qps = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(qps), burst)
}

// AWSMiddleware returns an aws-sdk-go-v2 middleware that blocks on
// limiter.Wait before letting a request proceed to the network, for
// exactly as long as it takes a token to become available or until the
// call's own context is done, whichever comes first -- so this composes
// correctly with storageReconcileTimeout/finalizerCleanupTimeout: a wait
// long enough to exceed those surfaces as a plain context.DeadlineExceeded,
// which classifyAWSStorageError/classifyAkamaiStorageError already map to
// outcomeTimeout. No new outcome classification needed.
//
// name labels the forge_rate_limit_wait_duration_seconds observation this
// records for every call regardless of outcome -- e.g. "s3" or "iam" (see
// s3/client.go) -- so a specific surface's wait times can be told apart on
// the same shared metric, the tuning signal for that surface's
// *_RATE_LIMIT_QPS/_BURST env vars (see cmd/main.go).
//
// A nil limiter is a no-op (the request proceeds immediately, unlimited,
// and nothing is observed) rather than a panic -- the same defensive floor
// NewLimiter already applies to a misconfigured QPS/burst, extended to a
// caller that forgot to construct a limiter at all (e.g. a test building
// an ApplicationReconciler without setting its *RateLimiter fields).
//
// Deliberately attached via a client's own per-instance Options.APIOptions
// (see s3/client.go, Akamai-Obj-Str/client.go) rather than the shared
// aws.Config.APIOptions every client built from that config would
// otherwise inherit together -- that's what would collapse S3 and IAM (or
// Akamai's account and object-endpoint clients) onto one shared budget,
// throttling the higher-capacity one down to the tighter one's ceiling for
// no reason tied to its own real capacity.
func AWSMiddleware(limiter *rate.Limiter, name string) func(*smithymiddleware.Stack) error {
	return func(stack *smithymiddleware.Stack) error {
		return stack.Finalize.Add(
			smithymiddleware.FinalizeMiddlewareFunc("RateLimit", func(
				ctx context.Context,
				in smithymiddleware.FinalizeInput,
				next smithymiddleware.FinalizeHandler,
			) (smithymiddleware.FinalizeOutput, smithymiddleware.Metadata, error) {
				if limiter != nil {
					start := time.Now()
					err := limiter.Wait(ctx)
					forgemetrics.RateLimitWaitDuration.WithLabelValues(name).Observe(time.Since(start).Seconds())
					if err != nil {
						return smithymiddleware.FinalizeOutput{}, smithymiddleware.Metadata{}, fmt.Errorf("rate limit wait: %w", err)
					}
				}

				return next.HandleFinalize(ctx, in)
			}),
			smithymiddleware.Before,
		)
	}
}

// RoundTripper is AWSMiddleware's plain net/http equivalent, for clients
// that aren't aws-sdk-go-v2-based -- specifically linodego's account API
// client (see Akamai-Obj-Str/client.go), which only ever accepts a
// *http.Client to instrument.
type RoundTripper struct {
	limiter *rate.Limiter
	next    http.RoundTripper
	name    string
}

// NewRoundTripper wraps next so every request waits on limiter first. name
// labels the forge_rate_limit_wait_duration_seconds observation this
// records, the same role it plays for AWSMiddleware. A nil next defaults
// to http.DefaultTransport, matching how an unset http.Client.Transport
// already behaves. A nil limiter is a no-op, the same defensive floor
// AWSMiddleware applies.
func NewRoundTripper(limiter *rate.Limiter, next http.RoundTripper, name string) *RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &RoundTripper{
		limiter: limiter,
		next:    next,
		name:    name,
	}
}

func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.limiter != nil {
		start := time.Now()
		err := rt.limiter.Wait(req.Context())
		forgemetrics.RateLimitWaitDuration.WithLabelValues(rt.name).Observe(time.Since(start).Seconds())
		if err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}
	}
	return rt.next.RoundTrip(req)
}

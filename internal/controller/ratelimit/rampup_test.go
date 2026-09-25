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
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRampUp_EndsAtOriginalTargetLimitAndBurst(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(100), 50)

	RampUp(context.Background(), limiter, 20*time.Millisecond)

	if limiter.Limit() != rate.Limit(100) {
		t.Fatalf("expected limit to end back at target 100, got %v", limiter.Limit())
	}
	if limiter.Burst() != 50 {
		t.Fatalf("expected burst to end back at target 50, got %d", limiter.Burst())
	}
}

func TestRampUp_LowersImmediatelyAndReturnsEarlyOnCancelledContext(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(100), 50)

	// RampUp applies its initial lowered values synchronously, before its
	// first select on ctx.Done() vs the ramp ticker -- an already-cancelled
	// context deterministically wins that first select (the ticker can't
	// have fired yet), so this exercises both the initial lowering and the
	// early-return path without relying on real-time sleeps.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	RampUp(ctx, limiter, time.Hour)

	if got, want := limiter.Limit(), rate.Limit(100)*rampStartFraction; got != want {
		t.Fatalf("expected limit lowered to start fraction %v, got %v", want, got)
	}
	if got, want := limiter.Burst(), int(50*rampStartFraction); got != want {
		t.Fatalf("expected burst lowered to start fraction %d, got %d", want, got)
	}
}

func TestRampUp_FloorsBurstAtOneForSmallTargets(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(1), 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	RampUp(ctx, limiter, time.Hour)

	if limiter.Burst() < 1 {
		t.Fatalf("expected burst to never floor below 1, got %d", limiter.Burst())
	}
}

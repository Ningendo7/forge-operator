package ratelimit

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// rampSteps and rampStartFraction: a freshly-elected leader can have a large
// backlog of Applications to reconcile all at once (informer cache sync,
// then the workqueue populating) -- starting at full burst lets that
// backlog fire as fast as the steady-state ceiling allows, right when a
// real provider rate limit is most likely to actually be hit. RampUp starts
// each limiter at rampStartFraction of its already-configured target and
// raises it in rampSteps increments back to that same target over
// rampDuration.
const (
	rampSteps         = 20
	rampStartFraction = 0.1
)

// RampUp reads limiter's current Limit/Burst as the target (whatever
// NewLimiter already resolved them to) and temporarily lowers it to
// rampStartFraction of that, then raises it back to the original target in
// rampSteps increments over rampDuration. Meant to be started once per
// limiter, right after a manager becomes leader (see cmd/main.go's use of
// mgr.Elected()) -- call it before anything else touches the limiter.
// Returns once fully ramped, or immediately if ctx is cancelled first.
func RampUp(ctx context.Context, limiter *rate.Limiter, rampDuration time.Duration) {
	targetLimit := limiter.Limit()
	targetBurst := limiter.Burst()
	stepDuration := rampDuration / rampSteps

	limiter.SetLimit(targetLimit * rampStartFraction)
	limiter.SetBurst(max(1, int(float64(targetBurst)*rampStartFraction)))

	ticker := time.NewTicker(stepDuration)
	defer ticker.Stop()

	for i := 1; i <= rampSteps; i++ {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			fraction := rampStartFraction + (1-rampStartFraction)*float64(i)/float64(rampSteps)
			limiter.SetLimitAt(now, targetLimit*rate.Limit(fraction))
			limiter.SetBurstAt(now, max(1, int(float64(targetBurst)*fraction)))
		}
	}

	now := time.Now()
	limiter.SetLimitAt(now, targetLimit)
	limiter.SetBurstAt(now, targetBurst)
}

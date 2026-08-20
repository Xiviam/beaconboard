package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingChecker struct {
	checks atomic.Int64
}

func (c *countingChecker) Check(_ context.Context, target Target) Result {
	c.checks.Add(1)
	return Result{
		TargetID:   target.ID,
		CheckedAt:  time.Now().UTC(),
		Latency:    time.Millisecond,
		StatusCode: 200,
		Healthy:    true,
	}
}

func TestSchedulerChecksImmediatelyAndPeriodically(t *testing.T) {
	target := Target{ID: "api", Interval: 10 * time.Millisecond}
	store := NewStore([]Target{target}, 10)
	checker := &countingChecker{}
	scheduler := NewScheduler([]Target{target}, checker, store)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	for checker.checks.Load() < 2 && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if checker.checks.Load() < 2 {
		t.Fatalf("checks = %d, want at least 2", checker.checks.Load())
	}
	detail, _ := store.Get(target.ID)
	if detail.Checks < 2 {
		t.Fatalf("stored checks = %d, want at least 2", detail.Checks)
	}
}

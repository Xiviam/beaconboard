package monitor

import (
	"context"
	"sync"
	"time"
)

// Scheduler runs one sequential check loop per target.
type Scheduler struct {
	targets []Target
	checker Checker
	store   *Store
}

// NewScheduler creates a concurrent target scheduler.
func NewScheduler(targets []Target, checker Checker, store *Store) *Scheduler {
	return &Scheduler{targets: targets, checker: checker, store: store}
}

// Run blocks until the context is cancelled and every target loop exits.
func (s *Scheduler) Run(ctx context.Context) {
	var group sync.WaitGroup
	group.Add(len(s.targets))
	for _, target := range s.targets {
		go func() {
			defer group.Done()
			s.runTarget(ctx, target)
		}()
	}
	group.Wait()
}

func (s *Scheduler) runTarget(ctx context.Context, target Target) {
	s.check(ctx, target)
	ticker := time.NewTicker(target.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.check(ctx, target)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) check(ctx context.Context, target Target) {
	result := s.checker.Check(ctx, target)
	if ctx.Err() == nil {
		s.store.Record(result)
	}
}

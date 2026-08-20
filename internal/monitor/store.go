package monitor

import (
	"sort"
	"sync"
)

const subscriberBufferSize = 256

type targetState struct {
	target   Target
	latest   Result
	hasCheck bool
	history  []Result
	checks   uint64
	failures uint64
}

// Store keeps bounded histories and fans out new results to subscribers.
type Store struct {
	mu           sync.RWMutex
	states       map[string]*targetState
	historyLimit int
	subscribers  map[chan Monitor]struct{}
}

// NewStore initializes state for every configured target.
func NewStore(targets []Target, historyLimit int) *Store {
	states := make(map[string]*targetState, len(targets))
	for _, target := range targets {
		states[target.ID] = &targetState{target: target}
	}
	return &Store{
		states:       states,
		historyLimit: historyLimit,
		subscribers:  make(map[chan Monitor]struct{}),
	}
}

// Record stores a result and publishes the resulting monitor snapshot.
func (s *Store) Record(result Result) {
	s.mu.Lock()
	state, ok := s.states[result.TargetID]
	if !ok {
		s.mu.Unlock()
		return
	}
	state.latest = result
	state.hasCheck = true
	state.checks++
	if !result.Healthy {
		state.failures++
	}
	state.history = append(state.history, result)
	if overflow := len(state.history) - s.historyLimit; overflow > 0 {
		copy(state.history, state.history[overflow:])
		state.history = state.history[:s.historyLimit]
	}
	monitor := state.monitor()
	for subscriber := range s.subscribers {
		select {
		case subscriber <- monitor:
		default:
			delete(s.subscribers, subscriber)
			close(subscriber)
		}
	}
	s.mu.Unlock()
}

// List returns stable, ID-sorted snapshots of all targets.
func (s *Store) List() []Monitor {
	s.mu.RLock()
	monitors := make([]Monitor, 0, len(s.states))
	for _, state := range s.states {
		monitors = append(monitors, state.monitor())
	}
	s.mu.RUnlock()
	sort.Slice(monitors, func(i, j int) bool { return monitors[i].ID < monitors[j].ID })
	return monitors
}

// Get returns one target and its rolling check history.
func (s *Store) Get(id string) (Detail, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[id]
	if !ok {
		return Detail{}, false
	}
	history := make([]Check, 0, len(state.history))
	for _, result := range state.history {
		history = append(history, result.check())
	}
	target := state.target
	return Detail{
		Monitor:        state.monitor(),
		Method:         target.Method,
		Interval:       target.Interval,
		Timeout:        target.Timeout,
		IntervalText:   target.Interval.String(),
		TimeoutText:    target.Timeout.String(),
		ExpectedStatus: target.ExpectedStatus,
		History:        history,
	}, true
}

// Subscribe creates a bounded stream of future monitor updates.
func (s *Store) Subscribe() (<-chan Monitor, func()) {
	updates := make(chan Monitor, subscriberBufferSize)
	s.mu.Lock()
	s.subscribers[updates] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return updates, func() {
		once.Do(func() {
			s.mu.Lock()
			if _, subscribed := s.subscribers[updates]; subscribed {
				delete(s.subscribers, updates)
				close(updates)
			}
			s.mu.Unlock()
		})
	}
}

func (s *targetState) monitor() Monitor {
	monitor := Monitor{
		ID:       s.target.ID,
		Name:     s.target.Name,
		URL:      s.target.URL,
		Pending:  !s.hasCheck,
		Checks:   s.checks,
		Failures: s.failures,
	}
	if !s.hasCheck {
		return monitor
	}
	checkedAt := s.latest.CheckedAt
	monitor.Healthy = s.latest.Healthy
	monitor.StatusCode = s.latest.StatusCode
	monitor.LatencyMS = durationMilliseconds(s.latest.Latency)
	monitor.CheckedAt = &checkedAt
	monitor.Error = s.latest.Error
	return monitor
}

func (r Result) check() Check {
	return Check{
		Healthy:    r.Healthy,
		StatusCode: r.StatusCode,
		LatencyMS:  durationMilliseconds(r.Latency),
		CheckedAt:  r.CheckedAt,
		Error:      r.Error,
	}
}

func durationMilliseconds(value interface{ Seconds() float64 }) float64 {
	return value.Seconds() * 1000
}

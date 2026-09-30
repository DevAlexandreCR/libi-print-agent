package agent

import (
	"sync"
	"time"
)

// Status is a point-in-time snapshot of the runner's state, for task 8.4's
// tray/status window.
type Status struct {
	Paired        bool
	Connected     bool
	LastError     string
	LastPrintedAt time.Time
}

// statusBox holds the current Status plus an optional change callback,
// guarded by one mutex so readers (Status()) and the update sites below
// never race.
type statusBox struct {
	mu       sync.Mutex
	current  Status
	onChange func(Status)
}

func (s *statusBox) get() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *statusBox) setOnChange(fn func(Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// update applies fn to the current status and, outside the lock, invokes
// the registered onChange callback with the new snapshot.
func (s *statusBox) update(fn func(*Status)) {
	s.mu.Lock()
	fn(&s.current)
	snapshot := s.current
	cb := s.onChange
	s.mu.Unlock()

	if cb != nil {
		cb(snapshot)
	}
}

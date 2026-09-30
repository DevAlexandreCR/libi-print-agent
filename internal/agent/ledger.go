package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// ledgerCapacity bounds the persisted ledger (task 8.3: "a persisted
// bounded set of acked job ids, e.g. last 500"). A var, not a const, so
// tests can shrink it without waiting on hundreds of jobs.
var ledgerCapacity = 500

const ledgerFileName = "print-ledger.json"

// ledgerEntry is one job's locally known outcome. It is written before the
// ack call (record) and updated after the ack succeeds (markAcked), so a
// crash between "printed the ticket" and "the server heard about it" can
// never cause a reprint: pullAndPrint sees Acked == false and only retries
// the ack.
type ledgerEntry struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"` // "printed" | "failed"
	Reason string `json:"reason,omitempty"`
	Acked  bool   `json:"acked"`
}

// ledger is a small FIFO-bounded, disk-persisted record of job outcomes
// under the agent's config directory. All exported methods lock mu.
type ledger struct {
	mu      sync.Mutex
	path    string
	entries []ledgerEntry
	index   map[string]int
}

// newLedger loads dir/print-ledger.json if present. A missing file means a
// fresh agent and is not an error; a corrupt file is treated the same way
// rather than risk the agent never starting again - the server's pending
// list remains the source of truth, so an empty ledger only risks
// reprinting whatever this agent printed in the seconds before the file
// was corrupted.
func newLedger(dir string) (*ledger, error) {
	l := &ledger{path: filepath.Join(dir, ledgerFileName), index: map[string]int{}}

	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}

	var entries []ledgerEntry
	if json.Unmarshal(data, &entries) != nil {
		return l, nil
	}
	l.entries = entries
	l.reindex()
	return l, nil
}

func (l *ledger) get(jobID string) (ledgerEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	i, ok := l.index[jobID]
	if !ok {
		return ledgerEntry{}, false
	}
	return l.entries[i], true
}

// record stores jobID's local outcome as unacked and persists immediately,
// before the caller acks it with the server.
func (l *ledger) record(jobID, status, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := ledgerEntry{JobID: jobID, Status: status, Reason: reason}
	if i, ok := l.index[jobID]; ok {
		l.entries[i] = entry
	} else {
		l.append(entry)
	}
	return l.persistLocked()
}

// markAcked flags jobID as acknowledged by the server and persists. A
// jobID this ledger has never seen is a no-op (nothing to mark).
func (l *ledger) markAcked(jobID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	i, ok := l.index[jobID]
	if !ok {
		return nil
	}
	l.entries[i].Acked = true
	return l.persistLocked()
}

// append adds an entry, evicting the oldest acked entry once over
// ledgerCapacity. An unacked entry is never evicted by age alone: it still
// needs its ack retried. If every entry were somehow unacked and still
// over capacity - not reachable in practice at 500 - the oldest is evicted
// anyway rather than growing the file without bound.
func (l *ledger) append(e ledgerEntry) {
	l.entries = append(l.entries, e)
	l.index[e.JobID] = len(l.entries) - 1
	if len(l.entries) <= ledgerCapacity {
		return
	}

	evict := 0
	for i, existing := range l.entries {
		if existing.Acked {
			evict = i
			break
		}
	}
	l.entries = append(l.entries[:evict], l.entries[evict+1:]...)
	l.reindex()
}

func (l *ledger) reindex() {
	l.index = make(map[string]int, len(l.entries))
	for i, e := range l.entries {
		l.index[e.JobID] = i
	}
}

// persistLocked writes the whole ledger via a temp-file rename, matching
// config.Save's crash-safety (internal/config/config.go).
func (l *ledger) persistLocked() error {
	data, err := json.Marshal(l.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

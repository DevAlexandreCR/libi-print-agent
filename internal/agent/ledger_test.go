package agent

import (
	"path/filepath"
	"testing"
)

func TestLedgerRecordGetMarkAcked(t *testing.T) {
	dir := t.TempDir()
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}

	if _, ok := l.get("j1"); ok {
		t.Fatalf("expected no entry for j1 before record")
	}

	if err := l.record("j1", "printed", ""); err != nil {
		t.Fatalf("record() error = %v", err)
	}
	entry, ok := l.get("j1")
	if !ok || entry.Status != "printed" || entry.Acked {
		t.Fatalf("unexpected entry after record: %+v, ok=%v", entry, ok)
	}

	if err := l.markAcked("j1"); err != nil {
		t.Fatalf("markAcked() error = %v", err)
	}
	entry, ok = l.get("j1")
	if !ok || !entry.Acked {
		t.Fatalf("expected j1 acked, got %+v", entry)
	}
}

func TestLedgerPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}
	if err := l.record("j1", "failed", "printer_not_found"); err != nil {
		t.Fatalf("record() error = %v", err)
	}

	l2, err := newLedger(dir)
	if err != nil {
		t.Fatalf("second newLedger() error = %v", err)
	}
	entry, ok := l2.get("j1")
	if !ok || entry.Status != "failed" || entry.Reason != "printer_not_found" {
		t.Fatalf("unexpected reloaded entry: %+v, ok=%v", entry, ok)
	}
}

func TestLedgerMissingFileIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}
	if _, ok := l.get("anything"); ok {
		t.Fatalf("expected empty ledger")
	}
}

func TestLedgerEvictsOldestAckedOverCapacity(t *testing.T) {
	old := ledgerCapacity
	ledgerCapacity = 3
	defer func() { ledgerCapacity = old }()

	dir := t.TempDir()
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}

	for _, id := range []string{"j1", "j2", "j3"} {
		if err := l.record(id, "printed", ""); err != nil {
			t.Fatalf("record(%s) error = %v", id, err)
		}
		if err := l.markAcked(id); err != nil {
			t.Fatalf("markAcked(%s) error = %v", id, err)
		}
	}
	// A 4th entry pushes the ledger over capacity; j1 (oldest, acked) must
	// be evicted rather than j4 or an unacked entry.
	if err := l.record("j4", "printed", ""); err != nil {
		t.Fatalf("record(j4) error = %v", err)
	}

	if _, ok := l.get("j1"); ok {
		t.Fatalf("expected j1 evicted")
	}
	if _, ok := l.get("j4"); !ok {
		t.Fatalf("expected j4 present")
	}
	if len(l.entries) != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", len(l.entries))
	}
}

func TestLedgerNeverEvictsUnackedOverAckedAlternative(t *testing.T) {
	old := ledgerCapacity
	ledgerCapacity = 2
	defer func() { ledgerCapacity = old }()

	dir := t.TempDir()
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}

	// j1 stays unacked (as if its ack never reached the server).
	if err := l.record("j1", "printed", ""); err != nil {
		t.Fatalf("record(j1) error = %v", err)
	}
	if err := l.record("j2", "printed", ""); err != nil {
		t.Fatalf("record(j2) error = %v", err)
	}
	if err := l.markAcked("j2"); err != nil {
		t.Fatalf("markAcked(j2) error = %v", err)
	}
	if err := l.record("j3", "printed", ""); err != nil {
		t.Fatalf("record(j3) error = %v", err)
	}

	if _, ok := l.get("j1"); !ok {
		t.Fatalf("expected unacked j1 to survive eviction")
	}
	if _, ok := l.get("j2"); ok {
		t.Fatalf("expected acked j2 evicted instead of unacked j1")
	}
}

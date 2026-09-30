package singleinstance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadHandoffRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Handoff{Port: 54321, Token: "abc123"}
	if err := WriteHandoff(dir, want); err != nil {
		t.Fatalf("WriteHandoff() error = %v", err)
	}

	got, err := ReadHandoff(dir)
	if err != nil {
		t.Fatalf("ReadHandoff() error = %v", err)
	}
	if got != want {
		t.Fatalf("ReadHandoff() = %+v, want %+v", got, want)
	}

	wantURL := "http://127.0.0.1:54321/?t=abc123"
	if got.URL() != wantURL {
		t.Fatalf("URL() = %q, want %q", got.URL(), wantURL)
	}
}

func TestReadHandoffMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadHandoff(dir); err == nil {
		t.Fatal("ReadHandoff() expected error for missing file, got nil")
	}
}

func TestReadHandoffCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHandoff(dir, Handoff{Port: 1, Token: "x"}); err != nil {
		t.Fatalf("WriteHandoff() error = %v", err)
	}
	// Overwrite with garbage.
	if err := os.WriteFile(filepath.Join(dir, handoffFileName), []byte("not json"), 0o600); err != nil {
		t.Fatalf("overwrite handoff file error = %v", err)
	}
	if _, err := ReadHandoff(dir); err == nil {
		t.Fatal("ReadHandoff() expected error for corrupt file, got nil")
	}
}

func TestReadHandoffInvalidValues(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHandoff(dir, Handoff{Port: 0, Token: "x"}); err != nil {
		t.Fatalf("WriteHandoff() error = %v", err)
	}
	if _, err := ReadHandoff(dir); err == nil {
		t.Fatal("ReadHandoff() expected error for port 0, got nil")
	}

	if err := WriteHandoff(dir, Handoff{Port: 100, Token: ""}); err != nil {
		t.Fatalf("WriteHandoff() error = %v", err)
	}
	if _, err := ReadHandoff(dir); err == nil {
		t.Fatal("ReadHandoff() expected error for empty token, got nil")
	}
}

func TestRemoveHandoffMissingIsNotError(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveHandoff(dir); err != nil {
		t.Fatalf("RemoveHandoff() on missing file error = %v", err)
	}
}

func TestRemoveHandoffThenReadFails(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHandoff(dir, Handoff{Port: 1, Token: "x"}); err != nil {
		t.Fatalf("WriteHandoff() error = %v", err)
	}
	if err := RemoveHandoff(dir); err != nil {
		t.Fatalf("RemoveHandoff() error = %v", err)
	}
	if _, err := ReadHandoff(dir); err == nil {
		t.Fatal("ReadHandoff() expected error after RemoveHandoff, got nil")
	}
}

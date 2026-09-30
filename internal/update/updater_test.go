package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/libi/libi-print-agent/internal/api"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubVersionChecker struct {
	info *api.VersionInfo
	err  error
}

func (s stubVersionChecker) Version(context.Context) (*api.VersionInfo, error) {
	return s.info, s.err
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newUpdaterFixture lays out a fake "running executable" at dir/agent.exe
// and returns an Updater wired to a test HTTP server serving newBinary at
// /download, with signature verification and relaunch stubbed out (real
// Authenticode/exec.Command would fail or misbehave under `go test`).
func newUpdaterFixture(t *testing.T, newBinary []byte) (*Updater, *[]string /* relaunch calls */, string /* exePath */) {
	t.Helper()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "agent.exe")
	if err := os.WriteFile(exePath, []byte("old-binary-contents"), 0o755); err != nil {
		t.Fatalf("seed running exe: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(newBinary)
	}))
	t.Cleanup(srv.Close)

	version := "2.0.0"
	info := &api.VersionInfo{Version: &version, URL: srv.URL + "/download", SHA256: sha256Hex(newBinary)}

	var relaunchCalls []string
	u := NewUpdater(stubVersionChecker{info: info}, srv.Client(), "1.0.0", exePath, false, func() bool { return false }, testLogger())
	u.Relaunch = func(path string) error {
		relaunchCalls = append(relaunchCalls, path)
		return nil
	}
	return u, &relaunchCalls, exePath
}

func TestCheckAndApplyAppliesNewerVersion(t *testing.T) {
	newBinary := []byte("new-binary-contents")
	u, relaunchCalls, exePath := newUpdaterFixture(t, newBinary)

	applied := u.CheckAndApply(context.Background())
	if !applied {
		t.Fatal("CheckAndApply() = false, want true")
	}
	if len(*relaunchCalls) != 1 || (*relaunchCalls)[0] != exePath {
		t.Fatalf("relaunch calls = %v, want exactly one call with %q", *relaunchCalls, exePath)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read swapped exe: %v", err)
	}
	if string(got) != string(newBinary) {
		t.Fatalf("exe contents = %q, want %q", got, newBinary)
	}

	old, err := os.ReadFile(exePath + ".old")
	if err != nil {
		t.Fatalf("read .old exe: %v", err)
	}
	if string(old) != "old-binary-contents" {
		t.Fatalf(".old contents = %q, want original binary", old)
	}
}

func TestCheckAndApplyNoUpdateWhenUpToDate(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "agent.exe")
	if err := os.WriteFile(exePath, []byte("current"), 0o755); err != nil {
		t.Fatalf("seed running exe: %v", err)
	}
	version := "1.0.0"
	u := NewUpdater(stubVersionChecker{info: &api.VersionInfo{Version: &version}}, nil, "1.0.0", exePath, false, func() bool { return false }, testLogger())
	relaunched := false
	u.Relaunch = func(string) error { relaunched = true; return nil }

	if applied := u.CheckAndApply(context.Background()); applied {
		t.Fatal("CheckAndApply() = true, want false when already up to date")
	}
	if relaunched {
		t.Fatal("Relaunch was called when no update was due")
	}
}

func TestCheckAndApplySkipsWhileBusy(t *testing.T) {
	newBinary := []byte("new-binary-contents")
	u, relaunchCalls, exePath := newUpdaterFixture(t, newBinary)
	u.IsBusy = func() bool { return true }

	if applied := u.CheckAndApply(context.Background()); applied {
		t.Fatal("CheckAndApply() = true, want false while busy")
	}
	if len(*relaunchCalls) != 0 {
		t.Fatal("Relaunch was called while a print job was in flight")
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(got) != "old-binary-contents" {
		t.Fatal("exe was swapped despite IsBusy returning true")
	}
}

func TestCheckAndApplyRejectsBadSHA256(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "agent.exe")
	if err := os.WriteFile(exePath, []byte("old-binary-contents"), 0o755); err != nil {
		t.Fatalf("seed running exe: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("new-binary-contents"))
	}))
	defer srv.Close()

	version := "2.0.0"
	info := &api.VersionInfo{Version: &version, URL: srv.URL, SHA256: "0000000000000000000000000000000000000000000000000000000000000"}
	u := NewUpdater(stubVersionChecker{info: info}, srv.Client(), "1.0.0", exePath, false, func() bool { return false }, testLogger())
	relaunched := false
	u.Relaunch = func(string) error { relaunched = true; return nil }

	if applied := u.CheckAndApply(context.Background()); applied {
		t.Fatal("CheckAndApply() = true, want false for a sha256 mismatch")
	}
	if relaunched {
		t.Fatal("Relaunch was called despite a sha256 mismatch")
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(got) != "old-binary-contents" {
		t.Fatal("exe was swapped despite a sha256 mismatch")
	}
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Fatal("a .old file was left behind after a failed (pre-swap) update")
	}
}

func TestCheckAndApplyRequiresSignatureWhenConfigured(t *testing.T) {
	newBinary := []byte("new-binary-contents")
	u, relaunchCalls, exePath := newUpdaterFixture(t, newBinary)
	u.RequireSignature = true
	u.VerifySignature = func(string) error { return errors.New("signature not trusted") }

	if applied := u.CheckAndApply(context.Background()); applied {
		t.Fatal("CheckAndApply() = true, want false when signature verification fails")
	}
	if len(*relaunchCalls) != 0 {
		t.Fatal("Relaunch was called despite failed signature verification")
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(got) != "old-binary-contents" {
		t.Fatal("exe was swapped despite failed signature verification")
	}
}

func TestCheckAndApplyLogsVersionCheckError(t *testing.T) {
	u := NewUpdater(stubVersionChecker{err: errors.New("network down")}, nil, "1.0.0", "unused", false, nil, testLogger())
	if applied := u.CheckAndApply(context.Background()); applied {
		t.Fatal("CheckAndApply() = true, want false when the version check fails")
	}
}

func TestCleanupStaleBinaryRemovesOldFile(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "agent.exe")
	oldPath := exePath + ".old"
	if err := os.WriteFile(oldPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed .old file: %v", err)
	}
	CleanupStaleBinary(exePath)
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("CleanupStaleBinary did not remove the .old file")
	}
	// Calling it again with nothing to remove must not error/panic.
	CleanupStaleBinary(exePath)
}

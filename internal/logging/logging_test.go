package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesToRotatedFile(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")

	logger, closer, err := New(logDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Registered after t.TempDir()'s own removal cleanup, so (t.Cleanup is
	// LIFO) this one runs first and releases the log file before Windows is
	// asked to remove the directory containing it.
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})

	logger.Info("agent starting", "version", "test")

	data, err := os.ReadFile(filepath.Join(logDir, "agent.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "agent starting") {
		t.Fatalf("log file does not contain expected message, got: %s", data)
	}
}

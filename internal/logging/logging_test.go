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

	logger, err := New(logDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.Info("agent starting", "version", "test")

	data, err := os.ReadFile(filepath.Join(logDir, "agent.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "agent starting") {
		t.Fatalf("log file does not contain expected message, got: %s", data)
	}
}

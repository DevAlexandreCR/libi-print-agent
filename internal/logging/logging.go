// Package logging sets up the agent's rolling file log
// (%ProgramData%\LiBi\logs on Windows).
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	maxSizeMB  = 5
	maxBackups = 5
	maxAgeDays = 30
)

// New creates a structured (JSON) logger that writes to a size-rotated file
// under dir and, for local debugging, also to stdout. dir is created if
// missing. The returned io.Closer closes the underlying log file and must be
// closed before the process exits (or, in tests, before a t.TempDir()
// holding the log file is removed) so no file handle is left open.
func New(dir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}

	rotator := &lumberjack.Logger{
		Filename:   filepath.Join(dir, "agent.log"),
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   true,
	}

	handler := slog.NewJSONHandler(io.MultiWriter(rotator, os.Stdout), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler), rotator, nil
}

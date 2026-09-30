//go:build windows

package install

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/libi/libi-print-agent/internal/autostart"
)

// TargetPath returns the stable install location: %LOCALAPPDATA%\LiBi\
// libi-print-agent.exe. Overridable via LIBI_PRINT_AGENT_INSTALL_DIR for
// tests and local development, mirroring internal/config's Dir().
func TargetPath() (string, error) {
	dir := os.Getenv("LIBI_PRINT_AGENT_INSTALL_DIR")
	if dir == "" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("install: LOCALAPPDATA is not set")
		}
		dir = filepath.Join(base, "LiBi")
	}
	return filepath.Join(dir, "libi-print-agent.exe"), nil
}

// EnsureInstalled copies the running executable to TargetPath if it is not
// already running from there, registers startup-at-logon and relaunches
// from the new path. relaunched reports whether a new process was started;
// the caller must exit immediately (without running the normal agent/UI/
// tray startup) when it is true, since this process's job is done and the
// new one is taking over.
func EnsureInstalled(logger *slog.Logger) (relaunched bool, err error) {
	target, err := TargetPath()
	if err != nil {
		return false, err
	}

	current, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("install: resolve running executable: %w", err)
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return false, fmt.Errorf("install: resolve symlinks for %s: %w", current, err)
	}

	if samePath(current, target) {
		// Already running from the stable location: still (re-)register
		// startup so a previous failed/partial install self-heals, but
		// there is nothing to copy or relaunch.
		if err := autostart.Ensure(target, logger); err != nil {
			logger.Warn("autostart registration failed", "error", err)
		}
		return false, nil
	}

	if err := copyFile(current, target); err != nil {
		return false, fmt.Errorf("install: copy %s -> %s: %w", current, target, err)
	}
	logger.Info("installed to stable location", "from", current, "to", target)

	if err := autostart.Ensure(target, logger); err != nil {
		logger.Warn("autostart registration failed", "error", err)
	}

	cmd := exec.Command(target, os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("install: relaunch %s: %w", target, err)
	}
	return true, nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// copyFile writes src's contents to dst via a temp-file-then-rename dance
// in dst's own directory, so a reader never observes a half-written exe.
func copyFile(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(dir, "libi-print-agent-install-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("copy contents: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, dst, err)
	}
	return nil
}

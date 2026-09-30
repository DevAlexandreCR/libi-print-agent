//go:build windows

package autostart

import (
	"fmt"
	"log/slog"
	"os/exec"

	"golang.org/x/sys/windows/registry"
)

// Ensure registers exePath to start at logon, preferring an all-users Task
// Scheduler entry (survives even when no user is logged in yet to run the
// HKCU fallback) and falling back to a per-user Run key when schtasks
// fails (typically because creating an ONLOGON/any-user task needs
// administrator rights the installer does not have). Both calls are
// idempotent (/F forces overwrite; registry.SetStringValue overwrites), so
// this is safe to call on every startup rather than only once.
func Ensure(exePath string, logger *slog.Logger) error {
	if err := ensureScheduledTask(exePath); err != nil {
		logger.Warn("schtasks startup registration failed, falling back to HKCU Run", "error", err)
		if fallbackErr := ensureRunKey(exePath); fallbackErr != nil {
			return fmt.Errorf("autostart: both schtasks and HKCU Run registration failed: schtasks=%v, run=%w", err, fallbackErr)
		}
		logger.Info("registered startup via HKCU Run fallback", "exePath", exePath)
		return nil
	}
	logger.Info("registered startup via Task Scheduler", "exePath", exePath)
	return nil
}

// ensureScheduledTask creates (or overwrites, via /F) an ONLOGON task that
// runs as any user with limited (non-elevated) rights, matching the agent's
// own permission level.
func ensureScheduledTask(exePath string) error {
	cmd := exec.Command("schtasks",
		"/Create",
		"/TN", TaskName,
		"/SC", "ONLOGON",
		"/TR", fmt.Sprintf("%q", exePath),
		"/RL", "LIMITED",
		"/F",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("autostart: schtasks /Create: %w: %s", err, string(out))
	}
	return nil
}

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// ensureRunKey writes a per-user Run value, the well-documented fallback
// autostart mechanism that needs no elevation.
func ensureRunKey(exePath string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open HKCU\\%s: %w", runKeyPath, err)
	}
	defer key.Close()

	if err := key.SetStringValue(TaskName, fmt.Sprintf("%q", exePath)); err != nil {
		return fmt.Errorf("autostart: set HKCU\\%s\\%s: %w", runKeyPath, TaskName, err)
	}
	return nil
}

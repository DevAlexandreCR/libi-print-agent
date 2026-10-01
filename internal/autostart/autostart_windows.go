//go:build windows

package autostart

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

const startupApprovedPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

// Ensure registers targetPath - the agent's stable install location (see
// internal/install.TargetPath; callers must never pass the path of a
// temporary/relocating process, e.g. one still running from Downloads) -
// to start at logon via the HKCU Run key (see this package's doc comment
// for why it is the only mechanism used).
//
// If targetPath does not exist yet, this is a no-op: pointing the Run
// entry at a file that isn't there would produce a confusing, broken
// autostart registration rather than fixing anything, so Ensure simply
// logs and returns nil for the caller to retry on a later startup.
//
// Idempotent (registry.SetStringValue overwrites and schtasks /Delete /F
// ignores a missing task), so it is safe - and expected - to call on
// every startup; this is also how a previously failed/partial
// registration self-heals.
func Ensure(targetPath string, logger *slog.Logger) error {
	if _, err := os.Stat(targetPath); err != nil {
		logger.Warn("autostart: install target not found yet, skipping registration", "target", targetPath, "error", err)
		return nil
	}

	removeLegacyScheduledTask(logger)

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open HKCU\\%s: %w", runKeyPath, err)
	}
	defer key.Close()

	// --autostart identifies a logon-triggered launch in the agent's own
	// logs (see cmd/libi-print-agent/main.go); it does not change startup
	// behavior - an already-paired agent never opens its status page on
	// its own regardless of how it was started, and an unpaired one
	// always does, since the merchant still has to pair it.
	value := runCommand(targetPath)
	if err := key.SetStringValue(TaskName, value); err != nil {
		return fmt.Errorf("autostart: set HKCU\\%s\\%s: %w", runKeyPath, TaskName, err)
	}
	logger.Info("registered startup via HKCU Run", "target", targetPath)
	return nil
}

// removeLegacyScheduledTask best-effort deletes the all-users ONLOGON
// scheduled task this agent used to create before this package switched to
// the HKCU Run key exclusively (see doc.go). schtasks exits non-zero when
// the task does not exist - the common case on any machine that never ran
// the old version, or already had this cleanup run once - which is logged
// at debug and never surfaced as an error to Ensure's caller.
func removeLegacyScheduledTask(logger *slog.Logger) {
	cmd := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		logger.Debug("autostart: no legacy scheduled task to remove (or removal failed)", "error", err, "output", string(out))
	} else {
		logger.Info("autostart: removed legacy scheduled task")
	}
}

// Status reports whether the Run entry Ensure registers is present and,
// if so, whether the user disabled it from Task Manager's Startup tab
// (see StateDisabledByUser).
func Status() (State, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return StateMissing, nil
		}
		return "", fmt.Errorf("autostart: open HKCU\\%s: %w", runKeyPath, err)
	}
	defer key.Close()

	if _, _, err := key.GetStringValue(TaskName); err != nil {
		if err == registry.ErrNotExist {
			return StateMissing, nil
		}
		return "", fmt.Errorf("autostart: read HKCU\\%s\\%s: %w", runKeyPath, TaskName, err)
	}

	approvedKey, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedPath, registry.QUERY_VALUE)
	if err != nil {
		// No StartupApproved entry at all: Task Manager has never
		// touched this one, so it is registered and enabled.
		return StateEnabled, nil
	}
	defer approvedKey.Close()

	data, _, err := approvedKey.GetBinaryValue(TaskName)
	if err != nil {
		return StateEnabled, nil
	}
	if enabled, ok := startupApprovedEnabled(data); ok && !enabled {
		return StateDisabledByUser, nil
	}
	return StateEnabled, nil
}

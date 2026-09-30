// Package install makes sure the agent runs from a stable, admin-free,
// self-update-writable location (task 8.4): %LOCALAPPDATA%\LiBi\
// libi-print-agent.exe. On first run from anywhere else (an installer
// drop, a support USB stick, a developer's Downloads folder) it copies
// itself there, registers startup (internal/autostart) and relaunches from
// the new path; a run that is already at that path is a no-op. Config
// stays under %ProgramData%\LiBi (internal/config) — a separate, per-machine
// location the self-update's binary swap never touches.
package install

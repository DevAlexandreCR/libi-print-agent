// Package autostart registers the agent to start at Windows logon (task
// 8.4) via the per-user HKCU\Software\Microsoft\Windows\CurrentVersion\Run
// key - the only mechanism this package uses.
//
// An earlier design preferred an all-users ONLOGON Task Scheduler entry,
// falling back to this same Run key only when creating the task failed.
// That was abandoned after a field report of the agent not starting after
// a reboot: a task created via `schtasks` defaults to "start only on AC
// power" / "stop if going on batteries", so it silently never runs on a
// laptop that boots on battery, and because schtasks "succeeded" in that
// case, the reliable Run-key fallback was never attempted. HKCU Run has
// neither failure mode and needs no elevation, so it is now the only path;
// Ensure also best-effort removes the legacy scheduled task so a machine
// upgraded from that version doesn't end up with two registered launchers.
package autostart

// TaskName is both the Run value name and the legacy Task Scheduler task
// name (so Ensure's cleanup can find and remove the latter).
const TaskName = "LiBi Impresion"

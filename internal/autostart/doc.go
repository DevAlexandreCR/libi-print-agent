// Package autostart registers the agent to start at Windows logon (task
// 8.4): a per-machine Task Scheduler entry (ONLOGON, any user), falling
// back to a per-user HKCU Run entry when creating the scheduled task needs
// elevation the installer does not have. Both paths are idempotent so
// calling Ensure on every startup is safe and self-healing.
package autostart

// TaskName is the Task Scheduler task name / Run value name used by both
// backends, so either one can find and clean up after the other.
const TaskName = "LiBi Impresion"

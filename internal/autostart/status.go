package autostart

// State is the result of Status(): whether the HKCU Run autostart entry
// (see this package's doc comment) is registered, and if so whether
// Windows will actually honor it at the next logon.
type State string

const (
	// StateEnabled means the Run value is present and nothing has
	// disabled it.
	StateEnabled State = "enabled"
	// StateDisabledByUser means the Run value is present but the user
	// turned it off from Task Manager's Startup tab (or Settings > Apps >
	// Startup apps on Windows 11) - Windows does not delete the Run value
	// for this, it only records the disable in a sibling
	// StartupApproved\Run value, so Ensure's next idempotent write would
	// otherwise look like a no-op while the entry quietly stays disabled.
	StateDisabledByUser State = "disabled_by_user"
	// StateMissing means there is no Run value for this agent at all
	// (never registered, Ensure has not run yet, or the non-Windows
	// stub).
	StateMissing State = "missing"
)

// runCommand is the HKCU Run value for targetPath. Windows command lines
// use plain double quotes with no escaping; Go's %q would double every
// backslash and leave Explorer unable to find the exe at logon.
func runCommand(targetPath string) string {
	return `"` + targetPath + `" --autostart`
}

// startupApprovedEnabled interprets the first byte of the
// StartupApproved\Run binary value Windows keeps per Run entry. This
// encoding is observed behavior, not documented by Microsoft:
// 0x02 or 0x06 = enabled (Task Manager / Startup apps shows "Enabled"),
// 0x03 or 0x07 = disabled. ok is false when data is empty or its first
// byte is none of these four values; callers should then treat the entry
// as enabled, since StartupApproved only exists to record an explicit
// user disable and an unrecognized value is not that.
func startupApprovedEnabled(data []byte) (enabled bool, ok bool) {
	if len(data) == 0 {
		return false, false
	}
	switch data[0] {
	case 0x02, 0x06:
		return true, true
	case 0x03, 0x07:
		return false, true
	default:
		return false, false
	}
}

package singleinstance

// mutexName is the well-known OS mutex name arbitrating single-instance-ness
// machine-wide (the "Global\" prefix on Windows makes it visible across
// user sessions, matching the at-logon-of-any-user startup model).
const mutexName = `Global\LiBiPrintAgent`

// Lock is an acquired single-instance lock; Release gives it up. Callers
// must call Release before replacing the process image (self-update,
// reinstall-and-relaunch) rather than relying on process exit, since the
// relaunched child may otherwise race the parent's not-yet-released mutex.
type Lock interface {
	Release()
}

// Acquire tries to become the single instance. acquired is false if another
// process already holds the lock, in which case lock is nil and the caller
// should hand off to that process instead of starting its own agent/UI/tray.
func Acquire() (lock Lock, acquired bool, err error) {
	return acquireOS()
}

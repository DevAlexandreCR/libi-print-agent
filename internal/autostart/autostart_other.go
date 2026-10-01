//go:build !windows

package autostart

import "log/slog"

// Ensure is a no-op on non-Windows platforms (the agent only ships for
// Windows; this exists so `go build`/`go vet`/`go test` run in the Linux
// build container).
func Ensure(targetPath string, logger *slog.Logger) error {
	return nil
}

// Status always reports StateMissing on non-Windows platforms; see Ensure.
func Status() (State, error) {
	return StateMissing, nil
}

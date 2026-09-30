//go:build !windows

package install

import "log/slog"

// EnsureInstalled is a no-op on non-Windows platforms (the agent only
// ships for Windows; this exists so `go build`/`go vet`/`go test` run in
// the Linux build container and so a developer can run the agent in place
// on macOS without it trying to relocate itself).
func EnsureInstalled(logger *slog.Logger) (relaunched bool, err error) {
	return false, nil
}

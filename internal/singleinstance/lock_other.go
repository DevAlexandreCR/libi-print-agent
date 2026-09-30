//go:build !windows

package singleinstance

// On non-Windows platforms (the agent only ships for Windows; this build
// exists so `go build`/`go vet`/`go test` run in the Linux build container)
// there is no real OS-level mutex to race over, so acquisition always
// succeeds and Release is a no-op.
type noopLock struct{}

func (noopLock) Release() {}

func acquireOS() (Lock, bool, error) {
	return noopLock{}, true, nil
}

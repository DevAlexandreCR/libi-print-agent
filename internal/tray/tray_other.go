//go:build !windows

package tray

import "context"

// Tray is a no-op on non-Windows platforms (the agent only ships for
// Windows; this exists so `go build`/`go vet`/`go test` run in the Linux
// build container). Loop simply blocks until ctx is cancelled.
type Tray struct{}

// New returns a no-op Tray.
func New(cb Callbacks) (*Tray, error) {
	return &Tray{}, nil
}

// Loop blocks until ctx is cancelled.
func (t *Tray) Loop(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// SetState is a no-op on this platform.
func (t *Tray) SetState(s State) {}

// SetStatusText is a no-op on this platform.
func (t *Tray) SetStatusText(s string) {}

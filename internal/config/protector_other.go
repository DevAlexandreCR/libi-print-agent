//go:build !windows

package config

import "errors"

// ErrDPAPIUnavailable is returned by dpapiProtector on non-Windows platforms.
var ErrDPAPIUnavailable = errors.New("config: DPAPI protection is only available on Windows")

type dpapiProtector struct{}

// NewDPAPIProtector returns the production Protector. On non-Windows
// platforms (this build tag) it compiles so `go vet`/`go test` run in the
// Linux container, but every call fails: the agent only ships for Windows.
func NewDPAPIProtector() Protector { return dpapiProtector{} }

func (dpapiProtector) Protect(_ []byte) ([]byte, error) { return nil, ErrDPAPIUnavailable }

func (dpapiProtector) Unprotect(_ []byte) ([]byte, error) { return nil, ErrDPAPIUnavailable }

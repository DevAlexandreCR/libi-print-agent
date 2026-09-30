//go:build !windows

package update

import "errors"

// ErrAuthenticodeUnsupported is returned by VerifyAuthenticode on
// non-Windows platforms. The agent only ships for Windows (design.md
// Non-Goals); this build exists so `go build`/`go vet`/`go test` run in the
// Linux build container. Production code only calls this when
// RequireSignature is true, which a non-Windows build never sets for real.
var ErrAuthenticodeUnsupported = errors.New("update: Authenticode verification is only available on Windows")

// VerifyAuthenticode always fails on this platform (build tag): see
// ErrAuthenticodeUnsupported.
func VerifyAuthenticode(path string) error {
	return ErrAuthenticodeUnsupported
}

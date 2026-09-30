//go:build !windows

package spool

import "errors"

// ErrUnsupported is returned by every call on the non-Windows stub. The
// agent only ships for Windows (design.md Non-Goals); this build exists so
// `go build`/`go vet`/`go test` succeed in the Linux build container used
// for CI and local development on this Mac.
var ErrUnsupported = errors.New("spool: winspool printing is only supported on Windows")

type unsupportedPrinter struct{}

// New returns the production Printer. On this platform (build tag) it
// compiles but every call fails with ErrUnsupported.
func New() Printer { return unsupportedPrinter{} }

func (unsupportedPrinter) Write(_, _ string, _ []byte) error { return ErrUnsupported }

func (unsupportedPrinter) List() ([]PrinterInfo, error) { return nil, ErrUnsupported }

// Package spool prints RAW ESC/POS bytes to a named Windows printer through
// the spooler and enumerates the printers installed on this machine,
// backing the "Agent printing contract" requirement in
// specs/print-agent/spec.md and design.md D1.
//
// New returns the real winspool-backed Printer on Windows (spool_windows.go)
// and an unsupported stub elsewhere (spool_other.go), so `go build`/`go
// vet`/`go test` run in the Linux build container used for CI and local
// development on this Mac (design.md Non-Goals: the agent only ships for
// Windows). Tests use NewFake instead of either (fake.go).
package spool

import "errors"

// ErrPrinterNotFound is returned (wrapped, with the printer name) by Write
// when printerName does not match any printer currently installed on this
// machine. It corresponds to the `printer_not_found` job-failure reason in
// the "Agent printing contract" requirement.
var ErrPrinterNotFound = errors.New("spool: printer not found")

// PrinterInfo describes one printer known to Windows. Field tags match the
// printer inventory shape the agent reports to the API (pair/heartbeat
// bodies, design.md D6/D8).
type PrinterInfo struct {
	Name       string `json:"name"`
	IsDefault  bool   `json:"isDefault"`
	DriverName string `json:"driverName,omitempty"`
}

// Printer prints RAW documents to a named Windows printer and lists the
// printers currently installed. Implemented by the winspool backend on
// Windows, a fake for tests, and an unsupported stub elsewhere.
type Printer interface {
	// Write submits data as a single RAW-datatype spool document named
	// docName to the printer named printerName. data must already contain
	// the paper cut and any other terminal bytes: Write never appends a
	// command (no cash-drawer kick, no extra cut) — see the "Agent printing
	// contract" requirement. Returns ErrPrinterNotFound if printerName is
	// not installed on this machine, or a wrapped spooler error otherwise.
	Write(printerName, docName string, data []byte) error

	// List returns the printers currently installed on this machine.
	List() ([]PrinterInfo, error)
}

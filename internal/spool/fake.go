package spool

import (
	"fmt"
	"sync"
)

// FakeJob records one accepted call to (*Fake).Write, for test assertions.
type FakeJob struct {
	PrinterName string
	DocName     string
	Data        []byte
}

// Fake is an in-memory Printer for tests: task 8.3's print loop and
// task 8.4's UI tests inject it instead of the real winspool backend (which
// only builds and runs on Windows). It behaves like a machine with a fixed
// printer inventory: Write against an unknown printer name fails exactly
// like the real backend does, with ErrPrinterNotFound.
type Fake struct {
	mu       sync.Mutex
	printers []PrinterInfo
	jobs     []FakeJob
	failWith error
}

// NewFake returns a Fake whose List() reports printers.
func NewFake(printers ...PrinterInfo) *Fake {
	return &Fake{printers: printers}
}

// Write implements Printer.Write.
func (f *Fake) Write(printerName, docName string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failWith != nil {
		return f.failWith
	}

	found := false
	for _, p := range f.printers {
		if p.Name == printerName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %q", ErrPrinterNotFound, printerName)
	}

	cp := make([]byte, len(data))
	copy(cp, data)
	f.jobs = append(f.jobs, FakeJob{PrinterName: printerName, DocName: docName, Data: cp})
	return nil
}

// List implements Printer.List.
func (f *Fake) List() ([]PrinterInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]PrinterInfo, len(f.printers))
	copy(out, f.printers)
	return out, nil
}

// FailNext makes every subsequent Write return err instead of recording a
// job, so tests can simulate a spooler error.
func (f *Fake) FailNext(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWith = err
}

// Jobs returns the jobs written so far, in the order they were written.
func (f *Fake) Jobs() []FakeJob {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]FakeJob, len(f.jobs))
	copy(out, f.jobs)
	return out
}

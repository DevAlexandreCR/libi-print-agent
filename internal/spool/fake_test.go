package spool

import (
	"errors"
	"testing"
)

func TestFakeWriteRecordsJob(t *testing.T) {
	f := NewFake(PrinterInfo{Name: "POS-80", IsDefault: true, DriverName: "Generic / Text Only"})

	if err := f.Write("POS-80", "kitchen ticket", []byte{0x1b, 0x40, 'h', 'i'}); err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}

	jobs := f.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("Jobs: got %d jobs, want 1", len(jobs))
	}
	got := jobs[0]
	if got.PrinterName != "POS-80" || got.DocName != "kitchen ticket" {
		t.Errorf("Jobs[0] = %+v, want PrinterName=POS-80 DocName=%q", got, "kitchen ticket")
	}
	if string(got.Data) != "\x1b\x40hi" {
		t.Errorf("Jobs[0].Data = %v, want the exact bytes passed to Write", got.Data)
	}
}

func TestFakeWriteUnknownPrinter(t *testing.T) {
	f := NewFake(PrinterInfo{Name: "POS-80"})

	err := f.Write("POS-58", "ticket", []byte("x"))
	if !errors.Is(err, ErrPrinterNotFound) {
		t.Fatalf("Write to unknown printer: err = %v, want ErrPrinterNotFound", err)
	}
	if len(f.Jobs()) != 0 {
		t.Errorf("Jobs: got %d jobs, want 0 (failed write must not record one)", len(f.Jobs()))
	}
}

func TestFakeFailNext(t *testing.T) {
	f := NewFake(PrinterInfo{Name: "POS-80"})
	spoolErr := errors.New("spooler is out of paper")
	f.FailNext(spoolErr)

	err := f.Write("POS-80", "ticket", []byte("x"))
	if !errors.Is(err, spoolErr) {
		t.Fatalf("Write after FailNext: err = %v, want %v", err, spoolErr)
	}
}

func TestFakeListReturnsIndependentCopy(t *testing.T) {
	f := NewFake(PrinterInfo{Name: "POS-80", IsDefault: true, DriverName: "Generic / Text Only"})

	printers, err := f.List()
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if len(printers) != 1 || printers[0].Name != "POS-80" {
		t.Fatalf("List() = %+v, want one printer named POS-80", printers)
	}

	printers[0].Name = "mutated"
	again, err := f.List()
	if err != nil {
		t.Fatalf("List: unexpected error: %v", err)
	}
	if again[0].Name != "POS-80" {
		t.Errorf("List() returned a slice aliasing internal state: got %q after caller mutation", again[0].Name)
	}
}

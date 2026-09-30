//go:build windows

package spool

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errInvalidPrinterName is ERROR_INVALID_PRINTER_NAME, the Win32 error
// OpenPrinterW returns for a printer name that is not installed on this
// machine. It is the signal Write maps to ErrPrinterNotFound.
const errInvalidPrinterName = syscall.Errno(1801)

const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004
	printerInfoLevel2      = 2
	rawDatatype            = "RAW"
)

// winspool.drv exports every proc this file needs (OpenPrinter, the doc/page
// lifecycle, WritePrinter, enumeration and the default-printer lookup),
// loaded lazily so the DLL is only touched when actually called.
var (
	modwinspool = windows.NewLazySystemDLL("winspool.drv")

	procOpenPrinterW       = modwinspool.NewProc("OpenPrinterW")
	procClosePrinter       = modwinspool.NewProc("ClosePrinter")
	procStartDocPrinterW   = modwinspool.NewProc("StartDocPrinterW")
	procStartPagePrinter   = modwinspool.NewProc("StartPagePrinter")
	procWritePrinter       = modwinspool.NewProc("WritePrinter")
	procEndPagePrinter     = modwinspool.NewProc("EndPagePrinter")
	procEndDocPrinter      = modwinspool.NewProc("EndDocPrinter")
	procEnumPrintersW      = modwinspool.NewProc("EnumPrintersW")
	procGetDefaultPrinterW = modwinspool.NewProc("GetDefaultPrinterW")
)

// docInfo1 mirrors the Win32 DOC_INFO_1 struct passed to StartDocPrinterW.
type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

// printerInfo2 mirrors the Win32 PRINTER_INFO_2 struct (level 2), used only
// to read the printer and driver names out of an EnumPrinters buffer. Field
// order and pointer-vs-DWORD sizing must match the C struct exactly since
// buffer entries are read via unsafe pointer arithmetic; fields this package
// never inspects (pDevMode, pSecurityDescriptor) are kept as uintptr so they
// still occupy the right number of bytes without being dereferenced.
type printerInfo2 struct {
	pServerName         *uint16
	pPrinterName        *uint16
	pShareName          *uint16
	pPortName           *uint16
	pDriverName         *uint16
	pComment            *uint16
	pLocation           *uint16
	pDevMode            uintptr
	pSepFile            *uint16
	pPrintProcessor     *uint16
	pDatatype           *uint16
	pParameters         *uint16
	pSecurityDescriptor uintptr
	Attributes          uint32
	Priority            uint32
	DefaultPriority     uint32
	StartTime           uint32
	UntilTime           uint32
	Status              uint32
	cJobs               uint32
	AveragePPM          uint32
}

type winspoolPrinter struct{}

// New returns the production, winspool-backed Printer.
func New() Printer { return winspoolPrinter{} }

// Write implements Printer.Write via OpenPrinterW / StartDocPrinterW
// (datatype RAW) / StartPagePrinter / WritePrinter (looped until every byte
// is accepted) / EndPagePrinter / EndDocPrinter / ClosePrinter. Every step
// that opened a resource is unwound via defer on every exit path, including
// a failure partway through the write loop; a failure in EndPagePrinter or
// EndDocPrinter is only surfaced if the job had otherwise succeeded, so the
// caller sees the first real error rather than a cleanup artifact.
func (winspoolPrinter) Write(printerName, docName string, data []byte) (err error) {
	printerNamePtr, e := windows.UTF16PtrFromString(printerName)
	if e != nil {
		return fmt.Errorf("spool: invalid printer name %q: %w", printerName, e)
	}

	var h windows.Handle
	if r, _, e := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(printerNamePtr)),
		uintptr(unsafe.Pointer(&h)),
		0, // pDefault: default access rights are enough to print
	); r == 0 {
		if errno, ok := e.(syscall.Errno); ok && errno == errInvalidPrinterName {
			return fmt.Errorf("%w: %q", ErrPrinterNotFound, printerName)
		}
		return fmt.Errorf("spool: OpenPrinter %q: %w", printerName, winErr(e))
	}
	defer procClosePrinter.Call(uintptr(h))

	docNamePtr, e := windows.UTF16PtrFromString(docName)
	if e != nil {
		return fmt.Errorf("spool: invalid doc name %q: %w", docName, e)
	}
	datatypePtr, e := windows.UTF16PtrFromString(rawDatatype)
	if e != nil {
		return fmt.Errorf("spool: raw datatype: %w", e)
	}
	info := docInfo1{pDocName: docNamePtr, pDatatype: datatypePtr}

	if r, _, e := procStartDocPrinterW.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&info))); r == 0 {
		return fmt.Errorf("spool: StartDocPrinter %q: %w", printerName, winErr(e))
	}
	defer func() {
		if r, _, e := procEndDocPrinter.Call(uintptr(h)); r == 0 && err == nil {
			err = fmt.Errorf("spool: EndDocPrinter %q: %w", printerName, winErr(e))
		}
	}()

	if r, _, e := procStartPagePrinter.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("spool: StartPagePrinter %q: %w", printerName, winErr(e))
	}
	defer func() {
		if r, _, e := procEndPagePrinter.Call(uintptr(h)); r == 0 && err == nil {
			err = fmt.Errorf("spool: EndPagePrinter %q: %w", printerName, winErr(e))
		}
	}()

	for written := 0; written < len(data); {
		var n uint32
		r, _, e := procWritePrinter.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&data[written])),
			uintptr(len(data)-written),
			uintptr(unsafe.Pointer(&n)),
		)
		if r == 0 {
			return fmt.Errorf("spool: WritePrinter %q: %w", printerName, winErr(e))
		}
		if n == 0 {
			return fmt.Errorf("spool: WritePrinter %q: spooler accepted 0 bytes", printerName)
		}
		written += int(n)
	}
	return nil
}

// List implements Printer.List via EnumPrintersW (local + persistent
// connections, level 2 for name and driver name) plus GetDefaultPrinterW to
// flag which one is the Windows default.
func (winspoolPrinter) List() ([]PrinterInfo, error) {
	defaultName, _ := getDefaultPrinterName() // best effort; "" if none set

	const flags = printerEnumLocal | printerEnumConnections

	var needed, returned uint32
	// First call with a nil buffer to learn the required size.
	procEnumPrintersW.Call(uintptr(flags), 0, printerInfoLevel2, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}

	buf := make([]byte, needed)
	r, _, e := procEnumPrintersW.Call(
		uintptr(flags), 0, printerInfoLevel2,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)),
	)
	if r == 0 {
		return nil, fmt.Errorf("spool: EnumPrinters: %w", winErr(e))
	}

	const entrySize = unsafe.Sizeof(printerInfo2{})
	out := make([]PrinterInfo, 0, returned)
	for i := uint32(0); i < returned; i++ {
		entry := (*printerInfo2)(unsafe.Pointer(&buf[uintptr(i)*entrySize]))
		name := utf16PtrToString(entry.pPrinterName)
		out = append(out, PrinterInfo{
			Name:       name,
			DriverName: utf16PtrToString(entry.pDriverName),
			IsDefault:  defaultName != "" && strings.EqualFold(name, defaultName),
		})
	}
	return out, nil
}

// getDefaultPrinterName wraps GetDefaultPrinterW's two-call size-then-fetch
// pattern. An empty result (no default printer configured) is not an error.
func getDefaultPrinterName() (string, error) {
	var needed uint32
	procGetDefaultPrinterW.Call(0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return "", nil
	}

	buf := make([]uint16, needed)
	r, _, e := procGetDefaultPrinterW.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&needed)),
	)
	if r == 0 {
		return "", fmt.Errorf("spool: GetDefaultPrinter: %w", winErr(e))
	}
	return windows.UTF16ToString(buf), nil
}

// utf16PtrToString converts a NUL-terminated UTF-16 string pointer, as
// returned inside an EnumPrinters buffer, into a Go string.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), uintptr(n)*2)) != 0 {
		n++
	}
	return windows.UTF16ToString(unsafe.Slice(p, n))
}

// winErr turns a LazyProc.Call error into a reportable error once the
// caller has already established failure via the BOOL/DWORD return value.
// Call always returns a non-nil error (Windows' last-error value), even
// when the call succeeded, so callers must check the return value first;
// this only normalizes the rare case where that value is itself
// ERROR_SUCCESS(0) into a generic error instead of a silent "0: 0".
func winErr(e error) error {
	if errno, ok := e.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return syscall.EINVAL
}

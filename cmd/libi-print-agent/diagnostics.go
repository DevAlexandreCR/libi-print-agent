// diagnostics.go implements the agent's hidden --list-printers and
// --print-test CLI flags: on-site verification tools for the D11 discovery
// checklist and for support, letting someone confirm winspool RAW printing
// behaves on a machine without a full paired agent running.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/libi/libi-print-agent/internal/spool"
)

// runDiagnostics handles --list-printers and --print-test. handled reports
// whether a diagnostic flag was recognized and acted on; the caller exits
// afterwards instead of starting the normal agent run loop.
func runDiagnostics(args []string) (handled bool, err error) {
	fs := flag.NewFlagSet("libi-print-agent", flag.ContinueOnError)
	listPrinters := fs.Bool("list-printers", false, "print the Windows printer inventory and exit")
	printTest := fs.String("print-test", "", "send a RAW ESC/POS test ticket to the named printer and exit")
	if err := fs.Parse(args); err != nil {
		return true, err
	}

	switch {
	case *listPrinters:
		return true, listPrintersCmd()
	case *printTest != "":
		return true, printTestCmd(*printTest)
	default:
		return false, nil
	}
}

func listPrintersCmd() error {
	printers, err := spool.New().List()
	if err != nil {
		return fmt.Errorf("list printers: %w", err)
	}
	if len(printers) == 0 {
		fmt.Fprintln(os.Stdout, "no printers found")
		return nil
	}
	for _, p := range printers {
		marker := ""
		if p.IsDefault {
			marker = " (default)"
		}
		fmt.Fprintf(os.Stdout, "%s%s\tdriver=%s\n", p.Name, marker, p.DriverName)
	}
	return nil
}

func printTestCmd(printerName string) error {
	if err := spool.New().Write(printerName, "LiBi test ticket", spool.TestTicketBytes()); err != nil {
		return fmt.Errorf("print test to %q: %w", printerName, err)
	}
	fmt.Fprintf(os.Stdout, "sent test ticket to %q\n", printerName)
	return nil
}

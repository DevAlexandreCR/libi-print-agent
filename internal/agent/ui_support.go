package agent

import (
	"fmt"
	"os"

	"github.com/libi/libi-print-agent/internal/config"
	"github.com/libi/libi-print-agent/internal/spool"
)

// Name returns the agent's display name as recorded at pairing time, or ""
// if unpaired. Used by task 8.4's status window.
func (r *Runner) Name() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Name
}

// Hostname returns this machine's hostname, used by the status window as
// the default "Nombre de este equipo" before the first pairing.
func (r *Runner) Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return h
}

// Printers lists the printers installed on this machine, for the status
// window's printer dropdown and printer list.
func (r *Runner) Printers() ([]spool.PrinterInfo, error) {
	return r.printer.List()
}

// TestPrint sends the shared RAW test ticket (spool.TestTicketBytes) to
// printerName, for the status window's "Imprimir prueba" button. It goes
// straight to the local spooler, bypassing the job/ack protocol entirely.
func (r *Runner) TestPrint(printerName string) error {
	return r.printer.Write(printerName, "LiBi test ticket", spool.TestTicketBytes())
}

// Unpair clears the locally stored token and agent identity and returns the
// runner to the unpaired state, cancelling any session in progress so Run's
// outer loop notices immediately instead of on the next disconnect. It is
// local-only (no server call): the spec's pairing-code/token model has no
// "unpair" endpoint, and a stale agent record on the server is harmless
// until the merchant admin revokes or reassigns it from the panel.
func (r *Runner) Unpair() error {
	r.mu.Lock()
	cleared := &config.Config{APIBaseURL: r.cfg.APIBaseURL}
	r.cfg = cleared
	r.mu.Unlock()

	if err := config.Save(cleared, r.protector); err != nil {
		return fmt.Errorf("agent: save config: %w", err)
	}
	r.client.SetToken("")

	r.sessionMu.Lock()
	cancel := r.sessionCancel
	r.sessionMu.Unlock()
	if cancel != nil {
		cancel()
	}

	r.status.update(func(s *Status) { s.Paired = false; s.Connected = false; s.LastError = "" })
	r.logger.Info("agent unpaired by user")
	return nil
}

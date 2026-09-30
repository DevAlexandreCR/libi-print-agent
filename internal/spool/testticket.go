package spool

// TestTicketBytes builds the shared RAW ESC/POS test document used by both
// the diagnostics --print-test CLI flag (D11 discovery checklist) and the
// status window's "Imprimir prueba" button (task 8.4): initialize, a
// human-readable line, feeds, then a full cut. It deliberately carries no
// cash-drawer command, matching the "Agent printing contract" requirement
// that the agent never adds one.
func TestTicketBytes() []byte {
	var b []byte
	b = append(b, 0x1B, 0x40) // ESC @ - initialize printer
	b = append(b, []byte("LIBI PRINT AGENT - TEST TICKET\n")...)
	b = append(b, []byte("If you can read this, RAW spooler printing works.\n")...)
	b = append(b, 0x0A, 0x0A, 0x0A, 0x0A) // feeds, so the cut clears the text
	b = append(b, 0x1D, 0x56, 0x00)       // GS V 0 - full cut
	return b
}

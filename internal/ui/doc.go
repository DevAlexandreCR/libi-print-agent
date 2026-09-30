// Package ui implements the local pairing/status page (task 8.4): a tiny
// HTTP server bound to 127.0.0.1 on a random port, protected by a random
// per-process token and a Host-header check (DNS rebinding protection), and
// a single embedded HTML/JS page that renders either the "vincular" form
// (unpaired) or the agent's status, printers and "desvincular" action
// (paired). cmd/libi-print-agent opens it in the default browser on first
// run and from the tray menu ("Abrir LiBi Impresión").
package ui

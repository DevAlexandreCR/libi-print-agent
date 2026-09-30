// Package api is the HTTP/SSE client to the LiBi API: pairing, heartbeat,
// the print_job wake-up stream with backoff reconnection, pending-job pull
// and ack (design.md D2/D6/D8, specs/print-agent and
// specs/print-job-dispatch). It holds no print or persistence logic of its
// own; internal/agent orchestrates it together with internal/spool and
// internal/config.
package api

// Package agent wires internal/api (transport), internal/config
// (persistence) and internal/spool (printing) into the print loop from
// design.md D2/D8 and the print-agent/print-job-dispatch specs: pull
// pending jobs on connect and on every print_job wake-up, print them in
// order on a single worker, ack each one, persist a bounded local ledger of
// outcomes so a job already printed is never printed twice, and fall back
// to the unpaired state on a revoked token (401).
package agent

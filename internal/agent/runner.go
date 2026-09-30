package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/libi/libi-print-agent/internal/api"
	"github.com/libi/libi-print-agent/internal/config"
	"github.com/libi/libi-print-agent/internal/spool"
)

// Intervals for the paired session. Vars, not consts, so tests can shrink
// them instead of waiting on real 30s/60s ticks.
var (
	heartbeatInterval   = 30 * time.Second
	safetyPullInterval  = 60 * time.Second
	pairingPollInterval = 2 * time.Second
)

// Runner owns one agent's identity (config), its Windows printer and the
// API client, and drives the print loop described in design.md D2: pull on
// connect and on every wake-up, print sequentially, ack, and fall back to
// the unpaired state on a 401.
type Runner struct {
	dir       string
	protector config.Protector
	printer   spool.Printer
	client    *api.Client
	logger    *slog.Logger
	version   string

	mu  sync.Mutex // guards cfg; client's own fields have their own lock
	cfg *config.Config

	ledger *ledger
	status statusBox

	pullMu sync.Mutex // serializes pullAndPrint: a single worker prints in order

	sessionMu     sync.Mutex
	sessionCancel context.CancelFunc // set while a paired session is running; see Unpair
}

// NewRunner builds a Runner over an already-loaded Config (config.Load) and
// creates/loads this agent's on-disk job ledger under dir (normally
// config.Dir()). A nil httpClient uses api.NewClient's default.
func NewRunner(dir string, cfg *config.Config, protector config.Protector, printer spool.Printer, httpClient *http.Client, version string, logger *slog.Logger) (*Runner, error) {
	l, err := newLedger(dir)
	if err != nil {
		return nil, fmt.Errorf("agent: load ledger: %w", err)
	}

	client := api.NewClient(cfg.APIBaseURL, httpClient)
	client.SetToken(cfg.Token)

	r := &Runner{
		dir:       dir,
		protector: protector,
		printer:   printer,
		client:    client,
		logger:    logger,
		version:   version,
		cfg:       cfg,
		ledger:    l,
	}
	r.status.current = Status{Paired: cfg.Paired()}
	return r, nil
}

// OnStatusChange registers fn to be called with every Status change. Set it
// before calling Run/Pair from another goroutine (task 8.4's tray/status
// window); there is no synchronization against a concurrent Set.
func (r *Runner) OnStatusChange(fn func(Status)) {
	r.status.setOnChange(fn)
}

// Status returns the current snapshot.
func (r *Runner) Status() Status {
	return r.status.get()
}

// Busy reports whether a print job is in flight right now (pullAndPrint
// holds pullMu for the whole pull-then-print-then-ack pass). Task 8.4's
// self-updater checks this before swapping the binary, so an update never
// lands mid-print.
func (r *Runner) Busy() bool {
	if r.pullMu.TryLock() {
		r.pullMu.Unlock()
		return false
	}
	return true
}

func (r *Runner) paired() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Paired()
}

// Pair redeems a pairing code against apiBaseURL and persists the result,
// switching the runner into the paired state. Safe to call once before Run,
// or concurrently while Run is in its unpaired wait loop - task 8.3's
// hidden --pair CLI flag and task 8.4's pairing window both use this path.
func (r *Runner) Pair(ctx context.Context, apiBaseURL, code, name string) error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	printers, err := r.printer.List()
	if err != nil {
		return fmt.Errorf("agent: list printers: %w", err)
	}

	r.client.SetBaseURL(apiBaseURL)
	result, err := r.client.Pair(ctx, api.PairRequest{
		Code:     code,
		Name:     name,
		Hostname: hostname,
		Version:  r.version,
		Printers: printers,
	})
	if err != nil {
		return err
	}

	newCfg := &config.Config{
		APIBaseURL: apiBaseURL,
		Token:      result.Token,
		AgentID:    result.AgentID,
		Name:       result.Name,
	}
	if err := config.Save(newCfg, r.protector); err != nil {
		return fmt.Errorf("agent: save config: %w", err)
	}

	r.mu.Lock()
	r.cfg = newCfg
	r.mu.Unlock()
	r.client.SetToken(result.Token)

	r.status.update(func(s *Status) { s.Paired = true; s.LastError = "" })
	r.logger.Info("paired", "agentId", result.AgentID, "merchantId", result.MerchantID, "name", result.Name)
	return nil
}

// Run blocks until ctx is cancelled. While unpaired it waits, polling for a
// concurrent Pair call; once paired it runs the full session (heartbeat,
// SSE stream, pull-and-print) until the token is revoked (401) or ctx is
// cancelled, then returns to waiting so a later re-pair resumes without a
// process restart.
func (r *Runner) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		if !r.paired() {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pairingPollInterval):
				continue
			}
		}

		r.runSession(ctx)
	}
}

// runSession runs one paired session: heartbeat loop, safety-pull loop and
// the SSE stream, all feeding a single wake channel that the loop below
// drains one at a time into pullAndPrint (the single worker). It returns
// when the session ends, either because ctx was cancelled or because the
// token was revoked (handleUnauthorized was called, which cancels
// sessionCtx so every goroutine here unwinds).
func (r *Runner) runSession(ctx context.Context) {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.sessionMu.Lock()
	r.sessionCancel = cancel
	r.sessionMu.Unlock()
	defer func() {
		r.sessionMu.Lock()
		r.sessionCancel = nil
		r.sessionMu.Unlock()
	}()

	wake := make(chan struct{}, 1)
	triggerWake := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.heartbeatLoop(sessionCtx, cancel) }()
	go func() { defer wg.Done(); r.safetyPullLoop(sessionCtx, triggerWake) }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := r.client.RunStream(sessionCtx, api.StreamCallbacks{
			OnConnect: func() {
				r.status.update(func(s *Status) { s.Connected = true })
				triggerWake() // pull pending on (re)connect
			},
			OnWakeup: triggerWake,
			OnDisconnect: func(err error) {
				r.status.update(func(s *Status) { s.Connected = false; s.LastError = err.Error() })
			},
		})
		if errors.Is(err, api.ErrUnauthorized) {
			r.handleUnauthorized()
		}
		cancel()
	}()

	for {
		select {
		case <-sessionCtx.Done():
			wg.Wait()
			r.status.update(func(s *Status) { s.Connected = false })
			return
		case <-wake:
			if err := r.pullAndPrint(sessionCtx); err != nil {
				if errors.Is(err, api.ErrUnauthorized) {
					r.handleUnauthorized()
					cancel()
					continue
				}
				r.logger.Warn("pull and print failed", "error", err)
				r.status.update(func(s *Status) { s.LastError = err.Error() })
			}
		}
	}
}

func (r *Runner) heartbeatLoop(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printers, err := r.printer.List()
			if err != nil {
				r.logger.Warn("list printers for heartbeat failed", "error", err)
				continue
			}
			err = r.client.Heartbeat(ctx, api.HeartbeatRequest{Printers: printers, Version: r.version})
			if errors.Is(err, api.ErrUnauthorized) {
				r.handleUnauthorized()
				cancel()
				return
			}
			if err != nil {
				r.logger.Warn("heartbeat failed", "error", err)
				r.status.update(func(s *Status) { s.LastError = err.Error() })
			}
		}
	}
}

func (r *Runner) safetyPullLoop(ctx context.Context, trigger func()) {
	ticker := time.NewTicker(safetyPullInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			trigger() // in case the SSE stream silently stalled
		}
	}
}

// handleUnauthorized clears the stored token and switches to the unpaired
// state ("Revoked token rejected" scenario). It is called from whichever
// goroutine first sees a 401; callers are responsible for then cancelling
// the session so the others unwind.
func (r *Runner) handleUnauthorized() {
	r.mu.Lock()
	cleared := &config.Config{APIBaseURL: r.cfg.APIBaseURL}
	r.cfg = cleared
	r.mu.Unlock()

	if err := config.Save(cleared, r.protector); err != nil {
		r.logger.Error("clear token after 401 failed", "error", err)
	}
	r.client.SetToken("")

	r.status.update(func(s *Status) { s.Paired = false; s.Connected = false; s.LastError = "token revoked" })
	r.logger.Warn("agent token revoked, returning to pairing state")
}

// pullAndPrint fetches this agent's pending jobs (oldest first - the
// server's source of truth) and handles them one at a time in order,
// serialized by pullMu so overlapping wake-ups/ticks coalesce into one pass
// instead of running concurrently (design.md D2: "single worker prints
// sequentially"). A job id already known to the ledger is never printed
// again; pullAndPrint only makes sure it is acked, which covers both a
// duplicate delivery and an ack that never reached the server before a
// restart.
func (r *Runner) pullAndPrint(ctx context.Context) error {
	r.pullMu.Lock()
	defer r.pullMu.Unlock()

	jobs, err := r.client.PendingJobs(ctx)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if entry, ok := r.ledger.get(job.ID); ok {
			if entry.Acked {
				continue
			}
			if err := r.ack(ctx, job.ID, entry.Status, entry.Reason); err != nil {
				return err
			}
			continue
		}

		status, reason := r.printJob(job)
		if err := r.ledger.record(job.ID, status, reason); err != nil {
			r.logger.Error("persist ledger entry failed", "jobId", job.ID, "error", err)
		}
		if status == string(api.JobPrinted) {
			r.status.update(func(s *Status) { s.LastPrintedAt = time.Now() })
		}
		if err := r.ack(ctx, job.ID, status, reason); err != nil {
			return err
		}
	}
	return nil
}

// ack reports a job's outcome and marks the ledger entry acked on success.
// A non-unauthorized failure (network, 5xx) is logged and left unacked: the
// next pull (a wake-up or the safety tick) retries it via the ledger branch
// in pullAndPrint, without reprinting.
func (r *Runner) ack(ctx context.Context, jobID, status, reason string) error {
	err := r.client.Ack(ctx, jobID, api.JobOutcome(status), reason)
	if err == nil {
		if e := r.ledger.markAcked(jobID); e != nil {
			r.logger.Error("mark ledger acked failed", "jobId", jobID, "error", e)
		}
		return nil
	}
	if errors.Is(err, api.ErrUnauthorized) {
		return err
	}
	r.logger.Warn("ack failed, will retry", "jobId", jobID, "status", status, "error", err)
	return nil
}

// printJob decodes and prints one job. It never returns an error: every
// failure becomes a "failed" outcome with a reason, per the "Agent
// printing contract" requirement's printer_not_found/spooler-error split.
func (r *Runner) printJob(job api.Job) (status, reason string) {
	data, err := base64.StdEncoding.DecodeString(job.PayloadBase64)
	if err != nil {
		r.logger.Error("invalid job payload", "jobId", job.ID, "error", err)
		return string(api.JobFailed), "invalid_payload"
	}

	if err := r.printer.Write(job.PrinterName, jobDocName(job), data); err != nil {
		if errors.Is(err, spool.ErrPrinterNotFound) {
			r.logger.Warn("printer not found", "jobId", job.ID, "printer", job.PrinterName)
			return string(api.JobFailed), "printer_not_found"
		}
		r.logger.Error("spooler write failed", "jobId", job.ID, "printer", job.PrinterName, "error", err)
		return string(api.JobFailed), "spooler_error: " + truncate(err.Error(), 200)
	}

	r.logger.Info("printed job", "jobId", job.ID, "printer", job.PrinterName, "ticketType", job.TicketType)
	return string(api.JobPrinted), ""
}

func jobDocName(job api.Job) string {
	if job.OrderNumber != nil {
		return fmt.Sprintf("LiBi %s #%d", job.TicketType, *job.OrderNumber)
	}
	return fmt.Sprintf("LiBi %s %s", job.TicketType, job.ID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

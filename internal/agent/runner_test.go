package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libi/libi-print-agent/internal/api"
	"github.com/libi/libi-print-agent/internal/config"
	"github.com/libi/libi-print-agent/internal/spool"
)

// This file is the contract test called for by task 8.3: the real add-print-
// agent API (pending/ack endpoints, task 5.2) does not exist yet, so a
// fakeAgentServer plays its documented contract (pair/heartbeat/pending/
// ack/stream/version) behind httptest instead of hitting a live local API.

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// --- fakeAgentServer: a minimal, in-memory stand-in for the print-agents
// API surface this task depends on. ---

type fakeJob struct {
	id, ticketType, printerName, payloadB64 string
	orderNumber                             *int64
	acked                                   bool
	status, reason                          string
}

type ackCall struct{ id, status, reason string }

type fakeAgentServer struct {
	mu          sync.Mutex
	validToken  string
	jobs        []*fakeJob
	acks        []ackCall
	heartbeats  int
	streamConns map[chan string]struct{}
}

func newFakeAgentServer() *fakeAgentServer {
	return &fakeAgentServer{streamConns: map[chan string]struct{}{}}
}

func (s *fakeAgentServer) setToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validToken = token
}

func (s *fakeAgentServer) authOK(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.validToken == "" {
		return false
	}
	return r.Header.Get("Authorization") == "Bearer "+s.validToken
}

func (s *fakeAgentServer) addJob(id, ticketType, printerName, payload string, orderNumber *int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &fakeJob{id: id, ticketType: ticketType, printerName: printerName, payloadB64: payload, orderNumber: orderNumber})
}

// pushWakeup simulates the API emitting a print_job SSE event to every
// currently connected agent stream.
func (s *fakeAgentServer) pushWakeup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.streamConns {
		select {
		case ch <- "event: print_job\ndata: x\n\n":
		default:
		}
	}
}

func (s *fakeAgentServer) ackCalls() []ackCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ackCall, len(s.acks))
	copy(out, s.acks)
	return out
}

func (s *fakeAgentServer) heartbeatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeats
}

func (s *fakeAgentServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/print-agents/pair", s.handlePair)
	mux.HandleFunc("/print-agents/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/print-agents/jobs/pending", s.handlePending)
	mux.HandleFunc("/print-agents/jobs/", s.handleAck)
	mux.HandleFunc("/print-agents/stream", s.handleStream)
	mux.HandleFunc("/print-agents/version", s.handleVersion)
	return mux
}

func (s *fakeAgentServer) handlePair(w http.ResponseWriter, r *http.Request) {
	var req api.PairRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Code != "123456" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "invalid or expired code"})
		return
	}
	s.setToken("tok-1")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(api.PairResult{Token: "tok-1", AgentID: "a1", MerchantID: "m1", Name: "Caja"})
}

func (s *fakeAgentServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	s.heartbeats++
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *fakeAgentServer) handlePending(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	out := []api.Job{}
	for _, j := range s.jobs {
		if j.acked {
			continue
		}
		out = append(out, api.Job{
			ID: j.id, TicketType: j.ticketType, PrinterName: j.printerName,
			OrderNumber: j.orderNumber, PayloadBase64: j.payloadB64,
		})
	}
	s.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string][]api.Job{"jobs": out})
}

func (s *fakeAgentServer) handleAck(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/print-agents/jobs/"), "/")
	if len(parts) != 2 || parts[1] != "ack" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id := parts[0]
	var body struct{ Status, Reason string }
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.id != id {
			continue
		}
		if !j.acked { // first outcome wins, matching the real API's idempotency
			j.acked = true
			j.status = body.Status
			j.reason = body.Reason
		}
		s.acks = append(s.acks, ackCall{id: id, status: body.Status, reason: body.Reason})
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *fakeAgentServer) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan string, 8)
	s.mu.Lock()
	s.streamConns[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.streamConns, ch)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-ch:
			fmt.Fprint(w, frame)
			flusher.Flush()
		}
	}
}

func (s *fakeAgentServer) handleVersion(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"version": nil})
}

// --- helpers ---

func withFastIntervals(t *testing.T) {
	t.Helper()
	oldHB, oldSafety, oldPoll := heartbeatInterval, safetyPullInterval, pairingPollInterval
	heartbeatInterval = 20 * time.Millisecond
	safetyPullInterval = 10 * time.Minute // large: isolates the wake-up path from the safety tick
	pairingPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		heartbeatInterval, safetyPullInterval, pairingPollInterval = oldHB, oldSafety, oldPoll
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- tests ---

func TestRunnerPairsPullsOnConnectAndOnWakeupInOrder(t *testing.T) {
	withFastIntervals(t)
	dir := t.TempDir()
	t.Setenv("LIBI_PRINT_AGENT_DIR", dir)

	server := newFakeAgentServer()
	srv := httptest.NewServer(server.handler())
	defer srv.Close()

	orderNum := int64(7)
	server.addJob("j1", "KITCHEN", "POS-58", b64("ticket-1"), nil)

	printer := spool.NewFake(
		spool.PrinterInfo{Name: "POS-58", IsDefault: true},
		spool.PrinterInfo{Name: "POS-80"},
	)

	cfg, err := config.Load(config.PlainProtector{})
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, printer, nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	if err := runner.Pair(ctx, srv.URL, "123456", "Caja"); err != nil {
		t.Fatalf("Pair() error = %v", err)
	}

	waitFor(t, 2*time.Second, func() bool { return len(printer.Jobs()) >= 1 }, "job j1 was not printed after pairing (pull on connect)")

	server.addJob("j2", "DELIVERY", "POS-80", b64("ticket-2"), &orderNum)
	server.pushWakeup()

	waitFor(t, 2*time.Second, func() bool { return len(printer.Jobs()) >= 2 }, "job j2 was not printed after a print_job wake-up")

	jobs := printer.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("expected exactly 2 printed jobs, got %d: %+v", len(jobs), jobs)
	}
	if jobs[0].PrinterName != "POS-58" || jobs[1].PrinterName != "POS-80" {
		t.Fatalf("jobs printed out of order: %+v", jobs)
	}

	waitFor(t, time.Second, func() bool { return len(server.ackCalls()) == 2 }, "server did not receive both acks")
	for _, ack := range server.ackCalls() {
		if ack.status != string(api.JobPrinted) {
			t.Fatalf("unexpected ack status %+v", ack)
		}
	}

	waitFor(t, time.Second, func() bool { return server.heartbeatCount() > 0 }, "expected at least one heartbeat")
	if !runner.Status().Paired || !runner.Status().Connected {
		t.Fatalf("unexpected status after session established: %+v", runner.Status())
	}
}

func TestRunnerDoesNotReprintAfterRestartWithUnackedLedgerEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIBI_PRINT_AGENT_DIR", dir)

	server := newFakeAgentServer()
	server.setToken("tok-1")
	server.addJob("j1", "KITCHEN", "POS-58", b64("ticket-1"), nil)
	srv := httptest.NewServer(server.handler())
	defer srv.Close()

	// Simulate a crash that happened after printJob wrote the ticket and
	// ledger.record persisted it, but before the ack request reached the
	// server: a fresh ledger already carries the unacked "printed" entry.
	l, err := newLedger(dir)
	if err != nil {
		t.Fatalf("newLedger() error = %v", err)
	}
	if err := l.record("j1", string(api.JobPrinted), ""); err != nil {
		t.Fatalf("pre-seed ledger record() error = %v", err)
	}

	printer := spool.NewFake(spool.PrinterInfo{Name: "POS-58", IsDefault: true})
	cfg := &config.Config{APIBaseURL: srv.URL, Token: "tok-1", AgentID: "a1", Name: "Caja"}
	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, printer, nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if err := runner.pullAndPrint(context.Background()); err != nil {
		t.Fatalf("pullAndPrint() error = %v", err)
	}

	if got := len(printer.Jobs()); got != 0 {
		t.Fatalf("expected no reprint of j1, got %d printed jobs", got)
	}
	acks := server.ackCalls()
	if len(acks) != 1 || acks[0].id != "j1" || acks[0].status != string(api.JobPrinted) {
		t.Fatalf("expected exactly one printed ack for j1, got %+v", acks)
	}
	// Reload from disk rather than reusing l: runner.pullAndPrint updates
	// its own in-memory ledger (loaded again inside NewRunner), and it is
	// that write-through-to-disk behavior under test here.
	reloaded, err := newLedger(dir)
	if err != nil {
		t.Fatalf("reload ledger error = %v", err)
	}
	entry, ok := reloaded.get("j1")
	if !ok || !entry.Acked {
		t.Fatalf("expected ledger entry for j1 to be acked on disk, got %+v, ok=%v", entry, ok)
	}
}

func TestRunnerUnauthorizedClearsTokenAndReturnsToUnpaired(t *testing.T) {
	withFastIntervals(t)
	dir := t.TempDir()
	t.Setenv("LIBI_PRINT_AGENT_DIR", dir)

	server := newFakeAgentServer()
	// validToken left empty: every authenticated call 401s, simulating a
	// revoked agent from the very first heartbeat/pull.
	srv := httptest.NewServer(server.handler())
	defer srv.Close()

	printer := spool.NewFake(spool.PrinterInfo{Name: "POS-58"})
	cfg := &config.Config{APIBaseURL: srv.URL, Token: "revoked-token", AgentID: "a1", Name: "Caja"}
	if err := config.Save(cfg, config.PlainProtector{}); err != nil {
		t.Fatalf("seed config.Save() error = %v", err)
	}

	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, printer, nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return !runner.Status().Paired }, "runner did not return to the unpaired state after 401")

	reloaded, err := config.Load(config.PlainProtector{})
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.Token != "" || reloaded.AgentID != "" {
		t.Fatalf("expected token/agentId cleared on disk, got %+v", reloaded)
	}
	// Regression: APIBaseURL used to survive this clear, so a revoked
	// token's saved base (e.g. a LAN test build's API) would be preferred
	// over the compiled-in default on the next pairing attempt.
	if reloaded.APIBaseURL != "" {
		t.Fatalf("expected APIBaseURL cleared on disk after a 401, got %+v", reloaded)
	}
	if got := runner.APIBase(); got != "https://api.libibot.com/api" {
		t.Fatalf("APIBase() after 401 = %q, want the compiled-in default", got)
	}
}

func TestRunnerAPIBaseUsesSavedBaseWhilePaired(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{APIBaseURL: "http://192.168.1.19:3001/api", Token: "tok-1", AgentID: "a1", Name: "Caja"}
	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, spool.NewFake(), nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if got := runner.APIBase(); got != "http://192.168.1.19:3001/api" {
		t.Fatalf("APIBase() = %q, want the saved base while paired", got)
	}
}

func TestRunnerAPIBaseFallsBackToDefaultWhenUnpairedDespiteStaleSavedBase(t *testing.T) {
	dir := t.TempDir()
	// Unpaired (no token/agentId) but still carrying a stale APIBaseURL,
	// e.g. a config file left over from a prior build before this fix.
	cfg := &config.Config{APIBaseURL: "http://192.168.1.19:3001/api"}
	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, spool.NewFake(), nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if got := runner.APIBase(); got != "https://api.libibot.com/api" {
		t.Fatalf("APIBase() = %q, want the compiled-in default when unpaired", got)
	}
}

func TestRunnerUnpairClearsAPIBaseURLAndSubsequentPairUsesDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIBI_PRINT_AGENT_DIR", dir)

	cfg := &config.Config{APIBaseURL: "http://192.168.1.19:3001/api", Token: "tok-1", AgentID: "a1", Name: "Caja"}
	runner, err := NewRunner(dir, cfg, config.PlainProtector{}, spool.NewFake(), nil, "1.0.0-test", "https://api.libibot.com/api", testLogger())
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if got := runner.APIBase(); got != "http://192.168.1.19:3001/api" {
		t.Fatalf("APIBase() before Unpair() = %q, want the saved base", got)
	}

	if err := runner.Unpair(); err != nil {
		t.Fatalf("Unpair() error = %v", err)
	}

	// The in-memory runner must now resolve to the default - this is what a
	// subsequent pairing attempt in the same process (e.g. via the status
	// page) would use (ui.Server.handlePair calls runner.APIBase()).
	if got := runner.APIBase(); got != "https://api.libibot.com/api" {
		t.Fatalf("APIBase() after Unpair() = %q, want the compiled-in default", got)
	}

	reloaded, err := config.Load(config.PlainProtector{})
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if reloaded.APIBaseURL != "" {
		t.Fatalf("expected APIBaseURL cleared on disk after Unpair(), got %+v", reloaded)
	}
}

func TestPrintJobOutcomeReasons(t *testing.T) {
	fake := spool.NewFake(spool.PrinterInfo{Name: "POS-58"})
	r := &Runner{logger: testLogger(), printer: fake}

	status, reason := r.printJob(api.Job{ID: "j1", TicketType: "KITCHEN", PrinterName: "POS-58", PayloadBase64: b64("hello")})
	if status != string(api.JobPrinted) || reason != "" {
		t.Fatalf("printed case: got status=%q reason=%q", status, reason)
	}

	status, reason = r.printJob(api.Job{ID: "j2", TicketType: "KITCHEN", PrinterName: "MISSING", PayloadBase64: b64("hi")})
	if status != string(api.JobFailed) || reason != "printer_not_found" {
		t.Fatalf("missing printer case: got status=%q reason=%q", status, reason)
	}

	status, reason = r.printJob(api.Job{ID: "j3", TicketType: "KITCHEN", PrinterName: "POS-58", PayloadBase64: "not-valid-base64!!"})
	if status != string(api.JobFailed) || reason != "invalid_payload" {
		t.Fatalf("invalid payload case: got status=%q reason=%q", status, reason)
	}

	fake.FailNext(errors.New("spooler is jammed"))
	status, reason = r.printJob(api.Job{ID: "j4", TicketType: "KITCHEN", PrinterName: "POS-58", PayloadBase64: b64("x")})
	if status != string(api.JobFailed) || !strings.HasPrefix(reason, "spooler_error: ") {
		t.Fatalf("spooler error case: got status=%q reason=%q", status, reason)
	}
}

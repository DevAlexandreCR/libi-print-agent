package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/libi/libi-print-agent/internal/api"
	"github.com/libi/libi-print-agent/internal/spool"
)

type fakeRunner struct {
	status       Status
	name         string
	hostname     string
	apiBase      string
	printers     []spool.PrinterInfo
	printersErr  error
	testPrintErr error
	pairErr      error
	unpairErr    error

	pairCalls   []pairCall
	unpairCalls int
	testPrints  []string
}

type pairCall struct {
	apiBaseURL, code, name string
}

func (f *fakeRunner) Status() Status   { return f.status }
func (f *fakeRunner) Name() string     { return f.name }
func (f *fakeRunner) Hostname() string { return f.hostname }
func (f *fakeRunner) APIBase() string  { return f.apiBase }
func (f *fakeRunner) Printers() ([]spool.PrinterInfo, error) {
	return f.printers, f.printersErr
}
func (f *fakeRunner) TestPrint(printerName string) error {
	f.testPrints = append(f.testPrints, printerName)
	return f.testPrintErr
}
func (f *fakeRunner) Pair(ctx context.Context, apiBaseURL, code, name string) error {
	f.pairCalls = append(f.pairCalls, pairCall{apiBaseURL, code, name})
	return f.pairErr
}
func (f *fakeRunner) Unpair() error {
	f.unpairCalls++
	return f.unpairErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer starts a real Server on a loopback listener so tests exercise
// the actual net/http request path (Host header included), not just the
// handlers in isolation. It defaults runner.apiBase when the test did not set
// one, since New no longer takes a base URL (Server resolves it fresh from
// runner.APIBase() on every pairing attempt instead - see server.go).
func newTestServer(t *testing.T, runner *fakeRunner) *Server {
	t.Helper()
	if runner.apiBase == "" {
		runner.apiBase = "https://api.example.com"
	}
	s, err := New(runner, "test-version", testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ln, err := s.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	go s.Serve(ln)
	t.Cleanup(func() { ln.Close() })
	return s
}

func get(t *testing.T, s *Server, path string, withToken bool) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", s.Port(), path)
	if withToken {
		sep := "?"
		if strings.ContainsRune(path, '?') {
			sep = "&"
		}
		url += sep + "t=" + s.Token()
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	return resp
}

func postJSON(t *testing.T, s *Server, path string, withToken bool, body any) *http.Response {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d%s", s.Port(), path)
	if withToken {
		url += "?t=" + s.Token()
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	resp, err := http.Post(url, "application/json", &buf)
	if err != nil {
		t.Fatalf("POST %s error = %v", url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestRequireAuthRejectsMissingOrWrongToken(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	if resp := get(t, s, "/api/status", false); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token: status = %d, want 403", resp.StatusCode)
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status?t=wrong-token", s.Port()))
	if err != nil {
		t.Fatalf("GET error = %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: status = %d, want 403", resp.StatusCode)
	}
}

func TestRequireAuthRejectsWrongHost(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/status?t=%s", s.Port(), s.Token()), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("spoofed Host: status = %d, want 403", resp.StatusCode)
	}
}

func TestHandleIndexServesPageWithValidToken(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := get(t, s, "/", true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("LiBi")) {
		t.Fatalf("index page does not look like the expected page: %s", body)
	}
}

func TestHandleStatusUnpaired(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1", status: Status{Paired: false}}
	s := newTestServer(t, runner)

	resp := get(t, s, "/api/status", true)
	var out statusResponse
	decodeJSON(t, resp, &out)
	if out.Paired {
		t.Fatal("expected paired=false")
	}
	if out.Hostname != "caja-1" {
		t.Fatalf("hostname = %q, want caja-1", out.Hostname)
	}
}

func TestHandleStatusIncludesAutostart(t *testing.T) {
	// autostart.Status()'s non-Windows stub always reports "missing" (see
	// internal/autostart/autostart_other.go), which is what this test
	// environment runs under; the Windows build is covered by
	// internal/autostart's own tests plus vet-windows type-checking the
	// registry code.
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := get(t, s, "/api/status", true)
	var out statusResponse
	decodeJSON(t, resp, &out)
	if out.Autostart != "missing" {
		t.Fatalf("autostart = %q, want %q", out.Autostart, "missing")
	}
}

func TestHandleStatusPairedIncludesPrinters(t *testing.T) {
	now := time.Now()
	runner := &fakeRunner{
		name:     "Caja",
		hostname: "caja-1",
		status:   Status{Paired: true, Connected: true, LastPrintedAt: now},
		printers: []spool.PrinterInfo{{Name: "POS-58", IsDefault: true}, {Name: "POS-80"}},
	}
	s := newTestServer(t, runner)

	resp := get(t, s, "/api/status", true)
	var out statusResponse
	decodeJSON(t, resp, &out)
	if !out.Paired || !out.Connected {
		t.Fatalf("unexpected status: %+v", out)
	}
	if out.Name != "Caja" {
		t.Fatalf("name = %q, want Caja", out.Name)
	}
	if out.Version != "test-version" {
		t.Fatalf("version = %q, want test-version", out.Version)
	}
	if len(out.Printers) != 2 || !out.Printers[0].IsDefault {
		t.Fatalf("unexpected printers: %+v", out.Printers)
	}
	if out.LastPrintedAt == "" {
		t.Fatal("expected lastPrintedAt to be set")
	}
}

func TestHandlePairSuccess(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/pair", true, pairRequest{Code: "123456", Name: "Caja"})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body=%s", resp.StatusCode, body)
	}
	if len(runner.pairCalls) != 1 {
		t.Fatalf("expected exactly one Pair() call, got %d", len(runner.pairCalls))
	}
	call := runner.pairCalls[0]
	if call.apiBaseURL != "https://api.example.com" || call.code != "123456" || call.name != "Caja" {
		t.Fatalf("unexpected pair call: %+v", call)
	}
}

func TestHandlePairResolvesAPIBaseFreshNotCachedAtConstruction(t *testing.T) {
	// Regression: Server used to capture apiBaseURL once in New() and reuse
	// it for every later pairing attempt. If the runner's effective base
	// changes after construction (e.g. an in-process unpair reset it to the
	// compiled-in default), a pairing attempt must pick up the new value
	// rather than the one in effect when the Server was built.
	runner := &fakeRunner{hostname: "caja-1", apiBase: "http://192.168.1.19:3001/api"}
	s := newTestServer(t, runner)

	runner.apiBase = "https://api.libibot.com/api"
	postJSON(t, s, "/api/pair", true, pairRequest{Code: "123456", Name: "Caja"})

	if len(runner.pairCalls) != 1 || runner.pairCalls[0].apiBaseURL != "https://api.libibot.com/api" {
		t.Fatalf("expected Pair() to use the runner's current APIBase(), got %+v", runner.pairCalls)
	}
}

func TestHandlePairDefaultsNameToHostname(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	postJSON(t, s, "/api/pair", true, pairRequest{Code: "123456", Name: ""})
	if len(runner.pairCalls) != 1 || runner.pairCalls[0].name != "caja-1" {
		t.Fatalf("expected name to default to hostname, got %+v", runner.pairCalls)
	}
}

func TestHandlePairRejectsShortCode(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/pair", true, pairRequest{Code: "123", Name: "Caja"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(runner.pairCalls) != 0 {
		t.Fatal("Pair() should not have been called for an invalid code")
	}
}

func TestHandlePairAlreadyPairedWarns(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1", status: Status{Paired: true}}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/pair", true, pairRequest{Code: "123456", Name: "Caja"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var out errorResponse
	decodeJSON(t, resp, &out)
	if out.Message == "" {
		t.Fatal("expected a warning message")
	}
	if len(runner.pairCalls) != 0 {
		t.Fatal("Pair() should not have been called while already paired")
	}
}

func TestMapPairError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"rate limited", api.ErrRateLimited, http.StatusTooManyRequests},
		{"invalid or expired code", &api.StatusError{StatusCode: http.StatusBadRequest, Message: "code expired"}, http.StatusBadRequest},
		{"network error", errors.New("dial tcp: connection refused"), http.StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, message := mapPairError(c.err)
			if status != c.wantStatus {
				t.Fatalf("status = %d, want %d", status, c.wantStatus)
			}
			if message == "" {
				t.Fatal("expected a non-empty message")
			}
		})
	}
}

func TestHandleUnpair(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1", status: Status{Paired: true}}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/unpair", true, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if runner.unpairCalls != 1 {
		t.Fatalf("expected exactly one Unpair() call, got %d", runner.unpairCalls)
	}
}

func TestHandleTestPrintSuccess(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/test-print", true, testPrintRequest{Printer: "POS-58"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(runner.testPrints) != 1 || runner.testPrints[0] != "POS-58" {
		t.Fatalf("unexpected test prints: %+v", runner.testPrints)
	}
}

func TestHandleTestPrintPrinterNotFound(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1", testPrintErr: fmt.Errorf("wrap: %w", spool.ErrPrinterNotFound)}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/test-print", true, testPrintRequest{Printer: "MISSING"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var out errorResponse
	decodeJSON(t, resp, &out)
	if out.Message != "Impresora no encontrada." {
		t.Fatalf("message = %q, want the printer-not-found message", out.Message)
	}
}

func TestHandleTestPrintMissingPrinterField(t *testing.T) {
	runner := &fakeRunner{hostname: "caja-1"}
	s := newTestServer(t, runner)

	resp := postJSON(t, s, "/api/test-print", true, testPrintRequest{Printer: ""})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(runner.testPrints) != 0 {
		t.Fatal("TestPrint() should not have been called without a printer name")
	}
}

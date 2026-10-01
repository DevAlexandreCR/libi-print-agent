package ui

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/libi/libi-print-agent/internal/api"
	"github.com/libi/libi-print-agent/internal/autostart"
	"github.com/libi/libi-print-agent/internal/spool"
)

//go:embed assets/index.html
var assets embed.FS

// Runner is the subset of *agent.Runner the status/pairing page needs. A
// narrow interface (rather than importing *agent.Runner's concrete type
// directly) keeps this package's tests independent of the real print loop.
// cmd/libi-print-agent wraps the real *agent.Runner in a small adapter that
// converts agent.Status into this package's Status (Go interface
// satisfaction requires identical method types, and a named struct in one
// package is never identical to a named struct in another even with the
// same fields).
type Runner interface {
	Status() Status
	Name() string
	Hostname() string
	Printers() ([]spool.PrinterInfo, error)
	TestPrint(printerName string) error
	// APIBase returns the API base URL to use for a pairing attempt right
	// now - the effective base (see config.EffectiveAPIBase), resolved
	// fresh rather than cached, so it reflects any Pair/Unpair that has
	// happened in this process since the Server was constructed.
	APIBase() string
	Pair(ctx context.Context, apiBaseURL, code, name string) error
	Unpair() error
}

// Status is this package's copy of agent.Status's shape (see Runner's doc
// comment for why it cannot just be that type).
type Status struct {
	Paired        bool
	Connected     bool
	LastError     string
	LastPrintedAt time.Time
}

// Server is the local pairing/status HTTP page (task 8.4): bound to
// 127.0.0.1 on a random port, every request gated by a random per-process
// token (in the URL, or the "t" query param on API calls) and by a Host
// header check that blocks DNS rebinding. Construct with New, bind a
// listener with Listen, then block in Serve.
type Server struct {
	runner  Runner
	version string
	logger  *slog.Logger
	token   string

	port          int
	expectedHosts map[string]struct{}
	mux           *http.ServeMux
}

// New builds a Server with a fresh random token. The base URL used for a
// pairing attempt from the page is resolved fresh from runner.APIBase() on
// every attempt rather than captured here, so it cannot go stale (see
// Runner.APIBase). version is the build's version string (main.version,
// "dev" if unset at build time); surfaced read-only in /api/status so the
// status page's footer can show which build is running.
func New(runner Runner, version string, logger *slog.Logger) (*Server, error) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("ui: generate token: %w", err)
	}

	s := &Server{
		runner:  runner,
		version: version,
		logger:  logger,
		token:   hex.EncodeToString(tokenBytes),
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", s.requireAuth(s.handleIndex))
	s.mux.HandleFunc("/api/status", s.requireAuth(s.handleStatus))
	s.mux.HandleFunc("/api/pair", s.requireAuth(s.handlePair))
	s.mux.HandleFunc("/api/unpair", s.requireAuth(s.handleUnpair))
	s.mux.HandleFunc("/api/test-print", s.requireAuth(s.handleTestPrint))
	return s, nil
}

// Token returns the per-process token, for main.go to build the tray's
// "Abrir" URL and the singleinstance handoff file.
func (s *Server) Token() string { return s.token }

// Port returns the bound port; valid only after Listen.
func (s *Server) Port() int { return s.port }

// URL returns the page's full localhost URL, token included.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", s.port, s.token)
}

// Listen binds 127.0.0.1 on a random port and records the Host header
// values Serve will accept (both the numeric loopback form and
// "localhost", since either is sent by browsers depending on how the URL
// was typed/opened).
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("ui: listen: %w", err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.expectedHosts = map[string]struct{}{
		fmt.Sprintf("127.0.0.1:%d", s.port): {},
		fmt.Sprintf("localhost:%d", s.port): {},
	}
	return ln, nil
}

// Serve blocks handling requests on ln until it is closed.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{Handler: s.mux, ReadHeaderTimeout: 5 * time.Second}
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// requireAuth wraps a handler with the Host-header check (DNS rebinding
// protection: a request whose Host does not match the exact loopback
// address/port we bound is rejected before it can reach any handler) and
// the per-process token check (query param "t", read by the embedded
// page's JS out of its own URL and echoed back on every API call).
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("t") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) hostAllowed(host string) bool {
	if s.expectedHosts == nil {
		return false // Listen was never called
	}
	_, ok := s.expectedHosts[host]
	return ok
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

type printerJSON struct {
	Name       string `json:"name"`
	IsDefault  bool   `json:"isDefault"`
	DriverName string `json:"driverName,omitempty"`
}

type statusResponse struct {
	Paired        bool          `json:"paired"`
	Connected     bool          `json:"connected"`
	LastError     string        `json:"lastError,omitempty"`
	LastPrintedAt string        `json:"lastPrintedAt,omitempty"`
	Name          string        `json:"name,omitempty"`
	Hostname      string        `json:"hostname"`
	Version       string        `json:"version,omitempty"`
	Printers      []printerJSON `json:"printers"`
	// Autostart is one of autostart.State's values ("enabled",
	// "disabled_by_user", "missing"); see autostart.Status. On a read
	// error it is left as "" rather than failing the whole status
	// response - the page's JS treats anything but "enabled"/
	// "disabled_by_user" as "not registered".
	Autostart string `json:"autostart"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.runner.Status()
	resp := statusResponse{
		Paired:    st.Paired,
		Connected: st.Connected,
		LastError: st.LastError,
		Name:      s.runner.Name(),
		Hostname:  s.runner.Hostname(),
		Version:   s.version,
	}
	if !st.LastPrintedAt.IsZero() {
		resp.LastPrintedAt = st.LastPrintedAt.Format(time.RFC3339)
	}

	if printers, err := s.runner.Printers(); err != nil {
		s.logger.Warn("ui: list printers for status page failed", "error", err)
	} else {
		for _, p := range printers {
			resp.Printers = append(resp.Printers, printerJSON{Name: p.Name, IsDefault: p.IsDefault, DriverName: p.DriverName})
		}
	}

	if state, err := autostart.Status(); err != nil {
		s.logger.Warn("ui: read autostart status for status page failed", "error", err)
	} else {
		resp.Autostart = string(state)
	}

	writeJSON(w, http.StatusOK, resp)
}

type pairRequest struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "method not allowed"})
		return
	}

	// Design.md risk "[Two agents on the same PC after a reinstall]": warn
	// rather than silently re-pair over an existing paired identity, so a
	// stale browser tab (or a race with a CLI -pair) cannot create a second
	// agent record for one machine without the user noticing.
	if s.runner.Status().Paired {
		writeJSON(w, http.StatusConflict, errorResponse{Message: "Este equipo ya está vinculado. Desvincúlalo primero si quieres vincularlo de nuevo."})
		return
	}

	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Message: "Solicitud inválida."})
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Code) != 6 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Message: "El código debe tener 6 dígitos."})
		return
	}
	if req.Name == "" {
		req.Name = s.runner.Hostname()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if err := s.runner.Pair(ctx, s.runner.APIBase(), req.Code, req.Name); err != nil {
		status, message := mapPairError(err)
		writeJSON(w, status, errorResponse{Message: message})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// mapPairError turns an internal/api error from Pair into the plain-
// language Spanish messages the pairing form is required to show
// (design.md: "código inválido o vencido, demasiados intentos, sin
// conexión").
func mapPairError(err error) (int, string) {
	if errors.Is(err, api.ErrRateLimited) {
		return http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos e inténtalo de nuevo."
	}
	var statusErr *api.StatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
		return http.StatusBadRequest, "Código inválido o vencido."
	}
	return http.StatusBadGateway, "Sin conexión con el servidor. Verifica tu internet e inténtalo de nuevo."
}

func (s *Server) handleUnpair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "method not allowed"})
		return
	}
	if err := s.runner.Unpair(); err != nil {
		s.logger.Error("ui: unpair failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Message: "No se pudo desvincular."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type testPrintRequest struct {
	Printer string `json:"printer"`
}

func (s *Server) handleTestPrint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "method not allowed"})
		return
	}
	var req testPrintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Printer) == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Message: "Selecciona una impresora."})
		return
	}
	if err := s.runner.TestPrint(req.Printer); err != nil {
		message := "Error de impresora: " + err.Error()
		if errors.Is(err, spool.ErrPrinterNotFound) {
			message = "Impresora no encontrada."
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Message: message})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type errorResponse struct {
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

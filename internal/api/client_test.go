package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libi/libi-print-agent/internal/spool"
)

func TestPairSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/print-agents/pair" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req PairRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Code != "123456" || req.Hostname != "caja-1" || len(req.Printers) != 1 {
			t.Fatalf("unexpected request body: %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(PairResult{Token: "tok", AgentID: "a1", MerchantID: "m1", Name: "Caja"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	result, err := c.Pair(context.Background(), PairRequest{
		Code: "123456", Hostname: "caja-1", Version: "1.0.0",
		Printers: []spool.PrinterInfo{{Name: "POS-80", IsDefault: true}},
	})
	if err != nil {
		t.Fatalf("Pair() error = %v", err)
	}
	if result.Token != "tok" || result.AgentID != "a1" || result.MerchantID != "m1" || result.Name != "Caja" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestPairBadCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "invalid or expired code"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Pair(context.Background(), PairRequest{Code: "000000"})
	var statusErr *StatusError
	if !isStatusError(err, &statusErr) || statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("Pair() error = %v, want *StatusError 400", err)
	}
}

func TestPairRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	_, err := c.Pair(context.Background(), PairRequest{Code: "123456"})
	if err != ErrRateLimited {
		t.Fatalf("Pair() error = %v, want ErrRateLimited", err)
	}
}

func TestHeartbeatUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer revoked" {
			t.Fatalf("missing/unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	c.SetToken("revoked")
	err := c.Heartbeat(context.Background(), HeartbeatRequest{Version: "1.0.0"})
	if err != ErrUnauthorized {
		t.Fatalf("Heartbeat() error = %v, want ErrUnauthorized", err)
	}
}

func TestPendingJobsSuccess(t *testing.T) {
	orderNumber := int64(42)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/print-agents/jobs/pending" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(pendingJobsResponse{Jobs: []Job{
			{ID: "j1", Seq: 1, TicketType: "KITCHEN", PrinterName: "POS-58", OrderNumber: &orderNumber, PayloadBase64: "AQI="},
		}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	c.SetToken("tok")
	jobs, err := c.PendingJobs(context.Background())
	if err != nil {
		t.Fatalf("PendingJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "j1" || jobs[0].OrderNumber == nil || *jobs[0].OrderNumber != 42 {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
}

func TestAckNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/print-agents/jobs/j1/ack" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	err := c.Ack(context.Background(), "j1", JobPrinted, "")
	if err != ErrJobNotFound {
		t.Fatalf("Ack() error = %v, want ErrJobNotFound", err)
	}
}

func TestAckDuplicateSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	for i := 0; i < 2; i++ {
		if err := c.Ack(context.Background(), "j1", JobPrinted, ""); err != nil {
			t.Fatalf("Ack() error = %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls reaching the server, got %d", calls)
	}
}

func TestVersionNullWhenUnpublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/print-agents/version" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("version must not require auth, got header %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"version":null}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	info, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if info.Version != nil {
		t.Fatalf("Version = %v, want nil", *info.Version)
	}
}

func TestVersionPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"1.2.0","url":"https://example/agent.exe","sha256":"abc"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	info, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if info.Version == nil || *info.Version != "1.2.0" || info.URL == "" || info.SHA256 == "" {
		t.Fatalf("unexpected version info: %+v", info)
	}
}

// isStatusError is a small helper so tests can assert on *StatusError
// without importing errors.As at every call site.
func isStatusError(err error, target **StatusError) bool {
	se, ok := err.(*StatusError)
	if !ok {
		return false
	}
	*target = se
	return true
}

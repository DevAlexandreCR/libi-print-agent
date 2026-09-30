package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRoundTripper serves canned SSE bodies without a real socket, so the
// reconnect test below exercises RunStream's parsing/backoff/callback wiring
// deterministically instead of racing real TCP connection teardown timing.
// bodies[i] is served for the (i+1)th call; once exhausted, "" (an
// immediate EOF) is served forever, which keeps the reconnect loop going
// fast so the test can just wait for a connect count.
type fakeRoundTripper struct {
	mu     sync.Mutex
	calls  int
	bodies []string
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()

	body := ""
	if idx < len(f.bodies) {
		body = f.bodies[idx]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func waitForCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
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

func TestRunStreamWakeupsAndReconnect(t *testing.T) {
	oldBase, oldCap := streamBackoffBase, streamBackoffCap
	streamBackoffBase, streamBackoffCap = time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { streamBackoffBase, streamBackoffCap = oldBase, oldCap })

	rt := &fakeRoundTripper{bodies: []string{
		": keepalive\n\nevent: print_job\ndata: ignored\n\nevent: print_job\ndata: ignored\n\n",
	}}
	c := NewClient("http://fake.invalid", &http.Client{Transport: rt})

	var mu sync.Mutex
	var wakeups, connects int
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.RunStream(ctx, StreamCallbacks{
			OnConnect: func() {
				mu.Lock()
				connects++
				mu.Unlock()
			},
			OnWakeup: func() {
				mu.Lock()
				wakeups++
				mu.Unlock()
			},
		})
	}()

	// The first body carries 2 wakeups then EOF; subsequent connections (the
	// fake's "" bodies) EOF immediately, so connects keeps climbing fast.
	// Reaching 2 connects proves RunStream reconnected after the first
	// stream ended, on its own, with backoff.
	waitForCond(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return connects >= 2 && wakeups >= 2
	}, "expected at least 2 connects and 2 wakeups")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStream() error = %v, want nil after ctx cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunStream did not return after ctx cancel")
	}
}

func TestRunStreamUnauthorizedStopsImmediately(t *testing.T) {
	var connects int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&connects, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, nil)
	err := c.RunStream(context.Background(), StreamCallbacks{})
	if err != ErrUnauthorized {
		t.Fatalf("RunStream() error = %v, want ErrUnauthorized", err)
	}
	if atomic.LoadInt32(&connects) != 1 {
		t.Fatalf("expected exactly 1 connection attempt, got %d", connects)
	}
}

func TestBackoffBoundsAndReset(t *testing.T) {
	b := newBackoff(time.Second, 60*time.Second)
	for i := 0; i < 10; i++ {
		d := b.Next()
		if d < 0 || d > 60*time.Second {
			t.Fatalf("Next() = %v out of [0, 60s] bounds at attempt %d", d, i)
		}
	}
	b.Reset()
	// After Reset, the very next delay is bounded by base (1s), not the
	// escalated value from the loop above.
	d := b.Next()
	if d > time.Second {
		t.Fatalf("Next() after Reset = %v, want <= 1s", d)
	}
}

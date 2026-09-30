package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// streamBackoffBase/Cap parameterize RunStream's reconnect backoff. Vars,
// not consts, so tests can shrink them instead of waiting on real seconds.
var (
	streamBackoffBase = time.Second
	streamBackoffCap  = 60 * time.Second
)

// errStreamClosed is the internal signal that one connection attempt ended
// without a real error (server closed it, or it reached EOF) and should be
// retried with backoff rather than treated as a terminal failure.
var errStreamClosed = errors.New("api: stream closed")

// StreamCallbacks receives lifecycle notifications from RunStream.
// OnWakeup fires for every print_job event (its data is not used - it is a
// pull signal, see "Delivery to the agent" requirement); OnConnect fires
// once per successful (re)connection; OnDisconnect fires with the error
// that ended a connection, right before backing off to reconnect.
type StreamCallbacks struct {
	OnConnect    func()
	OnWakeup     func()
	OnDisconnect func(error)
}

// RunStream keeps GET /print-agents/stream open until ctx is cancelled or
// the token is rejected, reconnecting on every other disconnect with
// exponential backoff and jitter, reset after each successful connect
// (design.md D2, "Reconnect after network loss" scenario). It returns nil
// only when ctx is cancelled; a rejected token returns ErrUnauthorized
// immediately, without retrying, since the same token will never succeed.
func (c *Client) RunStream(ctx context.Context, cb StreamCallbacks) error {
	b := newBackoff(streamBackoffBase, streamBackoffCap)

	for {
		if ctx.Err() != nil {
			return nil
		}

		wrapped := cb
		wrapped.OnConnect = func() {
			b.Reset()
			if cb.OnConnect != nil {
				cb.OnConnect()
			}
		}

		err := c.streamOnce(ctx, wrapped)
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if cb.OnDisconnect != nil {
			cb.OnDisconnect(err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(b.Next()):
		}
	}
}

// streamOnce performs a single connection attempt and blocks reading
// events until it ends, always returning a non-nil error describing why
// (errStreamClosed for a clean server-side close/EOF).
func (c *Client) streamOnce(ctx context.Context, cb StreamCallbacks) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/print-agents/stream", nil)
	if err != nil {
		return fmt.Errorf("api: build stream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken())
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return newStatusError(resp.StatusCode, body)
	}

	if cb.OnConnect != nil {
		cb.OnConnect()
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if event == "print_job" && cb.OnWakeup != nil {
				cb.OnWakeup()
			}
		default:
			// Blank lines (event boundaries), "data:"/"id:"/"retry:" lines and
			// ":"-prefixed comments/keepalives: nothing else to act on, a
			// print_job event's data is a wake-up only.
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errStreamClosed
}

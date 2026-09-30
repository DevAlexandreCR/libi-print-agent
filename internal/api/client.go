package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// requestTimeout bounds every ordinary (non-streaming) call. The stream
// itself is long-lived and deliberately not subject to it (see RunStream).
const requestTimeout = 15 * time.Second

// Client is the HTTP/SSE client to one LiBi API base URL, with a bearer
// token that can be changed at runtime (SetToken) as pairing and
// unauthorized-token handling require (design.md D6, D2).
type Client struct {
	httpClient *http.Client

	mu      sync.RWMutex
	baseURL string
	token   string
}

// NewClient returns a Client for baseURL (which already includes the API's
// path prefix, e.g. ".../api"). A nil httpClient uses a default one; note
// that client must not carry its own overall Timeout, since RunStream
// relies on a single request staying open indefinitely — requestTimeout is
// applied per call instead via context.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/")}
}

// SetBaseURL changes the API base URL (used once, by Pair, before the
// agent has a token to lose).
func (c *Client) SetBaseURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(baseURL, "/")
}

// SetToken changes the bearer token, e.g. after a successful Pair or to
// clear it on a 401 (ErrUnauthorized).
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *Client) base() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

func (c *Client) authToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// doJSON sends a JSON request (reqBody may be nil) and returns the raw
// response body and status code; callers interpret both per endpoint.
func (c *Client) doJSON(ctx context.Context, method, path string, reqBody any, auth bool) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return nil, 0, fmt.Errorf("api: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.authToken())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("api: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("api: read %s %s response: %w", method, path, err)
	}
	return data, resp.StatusCode, nil
}

// Pair redeems a pairing code (public endpoint, no token). See
// "Pairing code redemption" in specs/print-agent/spec.md.
func (c *Client) Pair(ctx context.Context, req PairRequest) (*PairResult, error) {
	data, status, err := c.doJSON(ctx, http.MethodPost, "/print-agents/pair", req, false)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusCreated:
		var out PairResult
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("api: decode pair response: %w", err)
		}
		return &out, nil
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, newStatusError(status, data)
	}
}

// Heartbeat reports printer inventory and version every 30s (design.md D2;
// "Printer inventory and heartbeat" requirement).
func (c *Client) Heartbeat(ctx context.Context, req HeartbeatRequest) error {
	data, status, err := c.doJSON(ctx, http.MethodPost, "/print-agents/heartbeat", req, true)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	default:
		return newStatusError(status, data)
	}
}

// PendingJobs returns this agent's pending jobs, oldest first: the source
// of truth pulled on connect and on every print_job wake-up ("Delivery to
// the agent" requirement).
func (c *Client) PendingJobs(ctx context.Context) ([]Job, error) {
	data, status, err := c.doJSON(ctx, http.MethodGet, "/print-agents/jobs/pending", nil, true)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		var out pendingJobsResponse
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("api: decode pending jobs: %w", err)
		}
		return out.Jobs, nil
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, newStatusError(status, data)
	}
}

// Ack reports a job's outcome. It is safe to call more than once for the
// same job id ("Acknowledgement and idempotency" requirement: the server
// keeps the first outcome).
func (c *Client) Ack(ctx context.Context, jobID string, status JobOutcome, reason string) error {
	data, code, err := c.doJSON(ctx, http.MethodPost,
		"/print-agents/jobs/"+url.PathEscape(jobID)+"/ack",
		ackRequest{Status: string(status), Reason: reason}, true)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrJobNotFound
	default:
		return newStatusError(code, data)
	}
}

// Version checks for a newer agent build (public endpoint, no token), so an
// unpaired agent can still self-update.
func (c *Client) Version(ctx context.Context) (*VersionInfo, error) {
	data, status, err := c.doJSON(ctx, http.MethodGet, "/print-agents/version", nil, false)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, newStatusError(status, data)
	}
	var out VersionInfo
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("api: decode version: %w", err)
	}
	return &out, nil
}

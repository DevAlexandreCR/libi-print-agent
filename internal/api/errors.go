package api

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnauthorized is returned by every authenticated call when the server
// answers 401: the token is unknown or belongs to a revoked agent
// ("Agent token authentication" requirement). Callers clear the stored
// token and return to the pairing state; retrying with the same token
// would never succeed.
var ErrUnauthorized = errors.New("api: unauthorized")

// ErrRateLimited is returned by Pair when the per-IP limiter rejects the
// request with 429.
var ErrRateLimited = errors.New("api: rate limited")

// ErrJobNotFound is returned by Ack when the job does not belong to this
// agent (or does not exist): the API answers 404 in that case.
var ErrJobNotFound = errors.New("api: job not found or not owned by this agent")

// StatusError wraps an otherwise-unhandled HTTP status from the API,
// carrying whatever {"message": "..."} body the API returned.
type StatusError struct {
	StatusCode int
	Message    string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("api: unexpected status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("api: unexpected status %d", e.StatusCode)
}

func newStatusError(status int, body []byte) error {
	var parsed struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed) // a non-JSON body just yields an empty message
	return &StatusError{StatusCode: status, Message: parsed.Message}
}

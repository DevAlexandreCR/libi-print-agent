package api

import (
	"math/rand"
	"time"
)

// maxBackoffAttempts caps the exponential growth's attempt counter so
// base<<attempt never has to be reasoned about near Go's (well-defined, but
// pointless past this) 64-bit shift range; the cap duration below is always
// reached well before this.
const maxBackoffAttempts = 32

// backoff computes reconnect delays for the SSE stream: exponential growth
// from base, capped at cap, with full jitter, resetting after a successful
// connection (design.md D2: "1s -> 60s cap, jitter, reset on success").
// Not safe for concurrent use; RunStream owns one per call.
type backoff struct {
	base, cap time.Duration
	attempt   int
}

func newBackoff(base, cap time.Duration) *backoff {
	return &backoff{base: base, cap: cap}
}

// Next returns the next delay and advances the attempt counter.
func (b *backoff) Next() time.Duration {
	d := b.base << b.attempt
	if d <= 0 || d > b.cap {
		d = b.cap
	}
	if b.attempt < maxBackoffAttempts {
		b.attempt++
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}

// Reset returns the next call to Next to the base delay, used after a
// stream connection succeeds so a later drop does not keep escalating.
func (b *backoff) Reset() {
	b.attempt = 0
}

package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Transfer-stall detection.
//
// http.Client.Timeout is a wall clock on the whole request, body included, so
// it punishes exactly the transfer that is working: a 599KB skill bundle
// arriving steadily over a slow link is killed at 30s mid-body, while a
// genuinely dead connection is held open for the same 30s (GH #7498). The
// honest question is not "has this taken too long?" but "is it still making
// progress?".
//
// So the client below demotes the wall clock to a backstop and makes progress
// the primary test: bounded connect and response-header phases, a body read
// that must keep producing bytes, and a ceiling loose enough that no healthy
// transfer reaches it. Slow-but-alive transfers finish; dead ones fail sooner
// than they used to.
//
// The mechanism is general and lives here for any caller. Adoption starts with
// the skill commands (see newSkillAPIClient) because that is where the failure
// was reported. Graduation is deliberately one edit: point NewAPIClient at
// NewStallAwareHTTPClient, fold StallAwareContext into APIContext, and delete
// the skill-local constructor. A permanent second HTTP client would leave the
// CLI with two timeout personalities, which is worse than either one alone.

const (
	// defaultStallTimeout is how long a transfer may produce no bytes at all
	// before it is considered dead. Tighter than the 30s total timeout it
	// replaces: with progress as the test, there is no reason to wait as long
	// as a whole slow download might legitimately take.
	defaultStallTimeout = 15 * time.Second

	// defaultTransferCeiling bounds a command that keeps making progress
	// forever (a server trickling bytes indefinitely). It is a backstop, not
	// the primary failure test, so it is loose enough that no healthy transfer
	// reaches it — but it is never absent.
	defaultTransferCeiling = 10 * time.Minute
)

// StallTimeout returns the no-progress budget for a single read.
//
// MULTICA_HTTP_STALL_TIMEOUT sets it directly. Failing that, an explicitly set
// MULTICA_HTTP_TIMEOUT is honored: on this path it keeps its plain-language
// meaning — the longest the user is willing to wait with nothing arriving —
// rather than silently becoming a total-elapsed limit again. With neither set,
// defaultStallTimeout applies.
func StallTimeout() time.Duration {
	if d, ok := durationFromEnv("MULTICA_HTTP_STALL_TIMEOUT"); ok {
		return d
	}
	if _, ok := durationFromEnv("MULTICA_HTTP_TIMEOUT"); ok {
		return httpTimeout()
	}
	return defaultStallTimeout
}

// TransferCeiling returns the hard upper bound for a stall-aware command. It
// never drops below a multiple of the stall budget, so raising the stall
// threshold cannot produce a ceiling that fires first and hides it.
func TransferCeiling() time.Duration {
	ceiling := defaultTransferCeiling
	if floor := 4 * StallTimeout(); floor > ceiling {
		ceiling = floor
	}
	return ceiling
}

// durationFromEnv parses a duration env var, accepting either a Go duration
// string ("45s", "2m") or a plain integer number of seconds ("45"), matching
// httpTimeout. It reports false for unset, empty, invalid, or non-positive
// values so callers can distinguish "user chose this" from "fall back".
func durationFromEnv(name string) (time.Duration, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d, true
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second, true
	}
	return 0, false
}

// StallError reports a transfer that stopped producing bytes. It is distinct
// from a deadline error on purpose: "no data for 15s after 200KB" and "the
// whole command hit its ceiling" point at different causes and different
// fixes, and collapsing them into one "request timed out" is what made #7498
// hard to diagnose from the client side.
type StallError struct {
	// Idle is the no-progress budget that was exceeded.
	Idle time.Duration
	// BytesRead is how much of the body had arrived before it stalled. Zero
	// means the body never started, which usually means the connection or the
	// server died rather than the link being slow.
	BytesRead int64
	Op        string // e.g. "GET /api/skills/abc" — shown only in --debug
	Err       error  // the underlying cancellation, for --debug
}

func (e *StallError) Error() string {
	msg := fmt.Sprintf("transfer stalled: no data received for %s after %d bytes", e.Idle, e.BytesRead)
	if e.Op != "" {
		return e.Op + ": " + msg
	}
	return msg
}

func (e *StallError) Unwrap() error { return e.Err }

// NewStallAwareHTTPClient builds the HTTP client described at the top of this
// file: bounded connect/TLS/header phases, a response body that fails when it
// stops making progress, and a loose whole-request ceiling behind both.
func NewStallAwareHTTPClient() *http.Client {
	idle := StallTimeout()
	base := &http.Transport{
		// ProxyFromEnvironment matches http.DefaultTransport. Dropping it
		// would silently break every user behind a corporate proxy, which is
		// a population that overlaps heavily with the slow links this change
		// exists for.
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: idle, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   idle,
		ExpectContinueTimeout: 1 * time.Second,
		// Covers the phase the body guard cannot see: connected, request
		// sent, server never replies.
		ResponseHeaderTimeout: idle,
	}
	return &http.Client{
		Transport: &stallGuardTransport{base: base, idle: idle},
		// The wall clock stays, but demoted from primary failure test to
		// backstop, and set loose enough that no healthy transfer reaches it.
		// Dropping it to 0 would have left a stalled request *upload* — which
		// the response-body guard cannot see — with no deadline at all, so
		// removing one wall clock would have quietly removed two.
		Timeout: TransferCeiling(),
	}
}

// StallAwareContext derives a command context for a client built by
// NewStallAwareHTTPClient. Like APIContext it leaves the transport's own
// deadline the one that fires, so the failure arrives as a classifiable
// network error rather than a bare context cancellation.
func StallAwareContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, TransferCeiling()+apiContextGrace)
}

// stallGuardTransport wraps every response body in a progress check.
type stallGuardTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *stallGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Canceling this context is what actually interrupts a blocked body read;
	// closing the body from another goroutine would race with the reader.
	// req.WithContext copies the request, so the caller's is untouched as the
	// RoundTripper contract requires.
	ctx, cancel := context.WithCancel(req.Context())
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	op := ""
	if req.URL != nil {
		op = req.Method + " " + req.URL.Path
	}
	resp.Body = newStallGuardBody(resp.Body, t.idle, cancel, op)
	return resp, nil
}

// stallGuardBody fails a read that has produced no bytes for idle.
//
// The timer re-checks rather than being reset on every Read: a Reset on the
// hot path races with the timer firing, and could kill a transfer that just
// delivered data. Here the timer wakes up, re-reads progress under the same
// lock that Read writes it, and either re-arms for the remaining time or
// trips. Cost is at most one extra wakeup per idle window.
type stallGuardBody struct {
	rc     io.ReadCloser
	idle   time.Duration
	cancel context.CancelFunc
	op     string
	timer  *time.Timer

	mu      sync.Mutex
	last    time.Time
	read    int64
	tripped bool
	closed  bool
}

func newStallGuardBody(rc io.ReadCloser, idle time.Duration, cancel context.CancelFunc, op string) *stallGuardBody {
	b := &stallGuardBody{rc: rc, idle: idle, cancel: cancel, op: op, last: time.Now()}
	b.timer = time.AfterFunc(idle, b.check)
	return b
}

func (b *stallGuardBody) check() {
	b.mu.Lock()
	if b.closed || b.tripped {
		b.mu.Unlock()
		return
	}
	if remaining := b.idle - time.Since(b.last); remaining > 0 {
		b.timer.Reset(remaining)
		b.mu.Unlock()
		return
	}
	b.tripped = true
	b.mu.Unlock()
	b.cancel()
}

func (b *stallGuardBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.mu.Lock()
		b.read += int64(n)
		b.last = time.Now()
		b.mu.Unlock()
	}
	if err != nil && err != io.EOF {
		if stalled := b.stallError(err); stalled != nil {
			return n, stalled
		}
	}
	return n, err
}

func (b *stallGuardBody) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.timer.Stop()
	err := b.rc.Close()
	// Release the derived context only after the body is done with it;
	// canceling earlier would abort the read this guard is protecting.
	b.cancel()
	return err
}

// stallError translates the cancellation the guard caused into the reason for
// it. Errors from any other cause pass through untouched.
func (b *stallGuardBody) stallError(cause error) *StallError {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.tripped {
		return nil
	}
	return &StallError{Idle: b.idle, BytesRead: b.read, Op: b.op, Err: cause}
}

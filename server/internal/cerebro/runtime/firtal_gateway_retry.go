package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// Transient transport-error retry for gateway calls (FIR-1561). A dropped
// connection to the AI gateway (EOF, connection reset/refused) used to surface
// to the user as a raw "call gateway: ...: EOF" run failure. The gateway
// container restarting mid-request is the common cause and almost always
// recovers within a second, so a couple of quick in-process retries make the
// blip invisible. Only transport errors (no HTTP response received) are
// retried — re-sending is safe because the request never completed. Sustained
// outages exhaust the retries and fall through to the auto-pause classifier
// (runtime_recovery), which pauses + auto-resumes instead of failing the run.
const (
	gatewayTransientMaxAttempts = 3
	gatewayTransientBaseBackoff = 500 * time.Millisecond
)

// doWithRetry runs req, retrying on transient transport errors. raw is the
// request body, re-wound on each attempt via GetBody. A nil error means a
// response (of any HTTP status) was received; status-level handling stays with
// the caller and is never retried here.
func (c *GatewayClient) doWithRetry(req *http.Request, raw []byte) (*http.Response, error) {
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	var (
		resp *http.Response
		err  error
	)
	for attempt := 1; attempt <= gatewayTransientMaxAttempts; attempt++ {
		if attempt > 1 {
			body, _ := req.GetBody()
			req.Body = body
		}
		resp, err = c.httpClient.Do(req)
		if err == nil || !isTransientTransportErr(err) {
			return resp, err
		}
		if req.Context().Err() != nil || attempt == gatewayTransientMaxAttempts {
			return resp, err
		}
		select {
		case <-req.Context().Done():
			return resp, err
		case <-time.After(time.Duration(attempt) * gatewayTransientBaseBackoff):
		}
	}
	return resp, err
}

// isTransientTransportErr reports whether err is a connection-level failure
// where no HTTP response was received — safe to retry an idempotent send.
// Caller-initiated cancellation and deadline expiry are never treated as
// transient: those mean "stop", not "the gateway blipped".
func isTransientTransportErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return matchesTransientTransportText(err.Error())
}

// matchesTransientTransportText is the string-level fallback for transport
// errors whose typed form was lost (e.g. read back from a stored run-failure
// message in the auto-pause classifier).
func matchesTransientTransportText(text string) bool {
	lower := strings.ToLower(text)
	for _, sig := range []string{
		"eof",
		"connection reset",
		"connection refused",
		"broken pipe",
		"server closed",
		"use of closed network connection",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// isTransientGatewayError reports whether a failed task's error text is a
// transient AI-gateway transport failure that survived the in-process retries
// — i.e. a sustained gateway outage rather than a one-off blip. The auto-pause
// classifier uses it to pause + auto-resume (runtime_recovery) instead of
// failing the run with the raw transport error. Gated on "gateway" so ordinary
// connection errors to other hosts are not swept in.
func isTransientGatewayError(errText string) bool {
	lower := strings.ToLower(errText)
	if !strings.Contains(lower, "gateway") {
		return false
	}
	return matchesTransientTransportText(lower)
}

// isProviderUnreachableError reports whether the agent process could not open
// a connection to its model provider at all — not a slow or half-broken call,
// but "no socket". isTransientGatewayError deliberately only covers our own
// AI gateway (it is gated on the word "gateway" so unrelated connection errors
// to other hosts are not swept in); a runtime talking straight to a provider
// emits none of those strings, so it was never pause-worthy (FIR-3651).
//
// That gap is expensive: a runtime in this state stays online and keeps
// claiming work, and every task it claims burns both of its attempts on the
// same error before surfacing red. Claude (sara.local) failed 14 of its last
// 15 runs this way on 2026-07-22 while Codex on the very same machine
// completed 212 runs in the same windows.
//
// The signatures below are deliberately narrow — each one means "the socket
// could not be opened", never "the request failed" — so a single unrelated
// blip cannot pause a healthy runtime. Callers pair it with the existing
// auto-pause backoff and circuit breaker, so a one-off still self-heals.
func isProviderUnreachableError(errText string) bool {
	lower := strings.ToLower(errText)
	for _, sig := range []string{
		// Bun/Node socket-open failure surfaced by the Claude Code CLI.
		"failedtoopensocket",
		"unable to connect to api",
		"no such host",
		"network is unreachable",
	} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

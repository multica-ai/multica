package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The CLI runs one process per command, so a single transport blip is a hard
// failure with no second chance: the agent or user sees an error and the work
// stops. That is what made a lossy link between a runtime and a self-hosted
// server look like constant outages (FLU-65) even while the server itself was
// healthy. retryTransport absorbs those blips underneath every APIClient call
// site at once, so no individual command has to remember to retry.
//
// Transport-layer failures and transient gateway responses are retried here.
// Gateway retries are limited to idempotent methods: replaying a 5xx POST
// could duplicate a side effect the server had in fact applied.

// defaultRetrySchedule is the backoff between attempts. N entries → N+1
// attempts in the worst case (one immediate + N retries). Kept short on
// purpose: the whole schedule has to fit inside the client's 30s timeout
// (see httpTimeout) alongside the attempts themselves, and a link that is
// still dropping packets after ~1s of backoff is an outage rather than a
// blip.
var defaultRetrySchedule = []time.Duration{
	200 * time.Millisecond,
	600 * time.Millisecond,
}

// retrySleep is the sleep between attempts. It is a package variable so tests
// can swap in an instant sleep without rewriting the schedule.
var retrySleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retrySchedule returns the backoff schedule, honoring MULTICA_HTTP_RETRIES.
// The value is the number of RETRIES (not attempts); 0 disables retrying
// entirely, which is the escape hatch for anyone who needs a command to fail
// fast. Invalid or negative values fall back to the default.
func retrySchedule() []time.Duration {
	v := strings.TrimSpace(os.Getenv("MULTICA_HTTP_RETRIES"))
	if v == "" {
		return defaultRetrySchedule
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultRetrySchedule
	}
	if n == 0 {
		return nil
	}
	schedule := make([]time.Duration, 0, n)
	delay := defaultRetrySchedule[0]
	for i := 0; i < n; i++ {
		schedule = append(schedule, delay)
		delay *= 3
	}
	return schedule
}

// retryTransport wraps a RoundTripper with bounded retries for transport
// errors. A nil base means http.DefaultTransport.
type retryTransport struct {
	base     http.RoundTripper
	schedule []time.Duration
}

// newRetryTransport builds the CLI's retrying transport over base.
func newRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{base: base, schedule: retrySchedule()}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	attemptReq := req
	for attempt := 0; ; attempt++ {
		resp, err := base.RoundTrip(attemptReq)
		if err == nil {
			if attempt >= len(t.schedule) || !shouldRetryStatus(attemptReq, resp) {
				return resp, nil
			}
			// Rewind before draining: if the body cannot be replayed we have
			// to hand this response back intact rather than an emptied one.
			next, rewindErr := rewindRequest(req)
			if rewindErr != nil {
				return resp, nil
			}
			// Drain so the connection goes back to the pool instead of being
			// torn down between attempts.
			drainAndClose(resp)
			if sleepErr := retrySleep(req.Context(), t.schedule[attempt]); sleepErr != nil {
				return nil, sleepErr
			}
			attemptReq = next
			continue
		}
		if attempt >= len(t.schedule) || !shouldRetry(attemptReq, err) {
			return nil, err
		}
		// A body we cannot rewind means the next attempt would send a
		// truncated request, which is worse than the error we already have.
		next, rewindErr := rewindRequest(req)
		if rewindErr != nil {
			return nil, err
		}
		if sleepErr := retrySleep(req.Context(), t.schedule[attempt]); sleepErr != nil {
			return nil, sleepErr
		}
		attemptReq = next
	}
}

// gatewayStatuses are the responses that mean "the edge is up but the
// application behind it is not" — exactly what a client sees while the stack
// is still coming up. On this deployment a 70-second cold start after a host
// reboot produced ~1000 of them (FLU-65), roughly half on plain GETs that
// were safe to repeat.
//
// 500 is deliberately absent: an application error is not a blip, and
// retrying it only delays a real failure. 429 is absent too — retrying a
// rate-limited request is what caused the limit.
var gatewayStatuses = map[int]bool{
	http.StatusBadGateway:         true,
	http.StatusServiceUnavailable: true,
	http.StatusGatewayTimeout:     true,
}

// shouldRetryStatus reports whether a successfully-received response should be
// thrown away and re-requested. Only idempotent methods qualify: a 502 says
// nothing about whether the request behind it was applied, so replaying a POST
// could still duplicate a write.
func shouldRetryStatus(req *http.Request, resp *http.Response) bool {
	if resp == nil || !gatewayStatuses[resp.StatusCode] {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	return isIdempotentMethod(req.Method)
}

// drainAndClose consumes a bounded amount of the response body so the
// underlying connection can be reused, then closes it.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// shouldRetry reports whether err is worth another attempt for this request.
//
// The safety question is whether replaying could duplicate a side effect. If
// the dial itself failed the server never saw the request, so any method can
// be replayed. Once bytes are on the wire we cannot tell "never processed"
// from "processed, response lost", so only methods the HTTP spec defines as
// idempotent are replayed — a dropped `issue comment add` reply is better
// than posting it twice.
func shouldRetry(req *http.Request, err error) bool {
	if err == nil {
		return false
	}
	// The caller's deadline is already blown (this is also how the client's
	// own Timeout surfaces); another attempt would only fail again.
	if req.Context().Err() != nil {
		return false
	}
	if !isRetryableTransportError(err) {
		return false
	}
	if isDialError(err) {
		return true
	}
	return isIdempotentMethod(req.Method)
}

// isRetryableTransportError filters out transport failures that a retry
// cannot fix: a bad certificate or an unresolvable name will fail identically
// on the next attempt, and burning the schedule on them only delays the error
// the user needs to see.
func isRetryableTransportError(err error) bool {
	switch classifyNetworkError(err) {
	case KindNetworkTLS, KindNetworkDNS:
		return false
	default:
		return true
	}
}

// isDialError reports whether the failure happened while establishing the
// connection, which proves the request never reached the server.
func isDialError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	return false
}

func isIdempotentMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// rewindRequest clones req with a fresh, unread body so it can be sent again.
// It reports an error when the body has already been consumed and cannot be
// replayed (GetBody is nil), which net/http only leaves unset for streaming
// bodies the CLI does not construct.
func rewindRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("request body cannot be replayed")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

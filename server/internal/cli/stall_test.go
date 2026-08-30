package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// trickleServer streams a JSON array in chunks, pausing between them, then
// optionally goes silent forever mid-body. It stands in for the link in GH
// #7498: bytes keep arriving, just not fast enough for a wall clock.
func trickleServer(t *testing.T, chunks int, pause time.Duration, stallAfter int) *httptest.Server {
	t.Helper()
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter is not a Flusher")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"items":[`)
		flusher.Flush()

		for i := range chunks {
			if stallAfter >= 0 && i == stallAfter {
				// Hold the response open without writing: the transfer is
				// alive at the socket level and producing nothing.
				select {
				case <-blocked:
				case <-r.Context().Done():
				}
				return
			}
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `"%s"`, strings.Repeat("x", 512))
			flusher.Flush()
			time.Sleep(pause)
		}
		fmt.Fprint(w, `]}`)
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

type trickleBody struct {
	Items []string `json:"items"`
}

// The regression #7498 reports: a transfer that is working the whole time,
// killed for taking too long in total. The stall-aware client completes the
// same download the total-elapsed client cannot.
func TestStallAwareClientCompletesSlowButSteadyTransfer(t *testing.T) {
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "400ms")
	const (
		chunks = 12
		pause  = 40 * time.Millisecond
	)

	t.Run("total-elapsed deadline kills a healthy transfer", func(t *testing.T) {
		srv := trickleServer(t, chunks, pause, -1)
		client := &APIClient{
			BaseURL:    srv.URL,
			HTTPClient: &http.Client{Timeout: 200 * time.Millisecond},
		}
		var out trickleBody
		err := client.GetJSON(context.Background(), "/slow", &out)
		if err == nil {
			t.Fatal("expected the total-elapsed client to fail; the fixture is not slow enough to be meaningful")
		}
	})

	t.Run("stall detection lets it finish", func(t *testing.T) {
		srv := trickleServer(t, chunks, pause, -1)
		client := &APIClient{BaseURL: srv.URL, HTTPClient: NewStallAwareHTTPClient()}

		var out trickleBody
		if err := client.GetJSON(context.Background(), "/slow", &out); err != nil {
			t.Fatalf("GetJSON: %v", err)
		}
		if len(out.Items) != chunks {
			t.Fatalf("got %d items, want %d", len(out.Items), chunks)
		}
	})
}

func TestStallAwareClientFailsOnStalledBody(t *testing.T) {
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "150ms")
	srv := trickleServer(t, 20, 10*time.Millisecond, 4)

	client := &APIClient{BaseURL: srv.URL, HTTPClient: NewStallAwareHTTPClient()}

	start := time.Now()
	var out trickleBody
	err := client.GetJSON(context.Background(), "/stalls", &out)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a stalled transfer to fail")
	}

	var stalled *StallError
	if !errors.As(err, &stalled) {
		t.Fatalf("error is not a *StallError: %#v", err)
	}
	// Bytes did arrive before the silence — this is the signal that separates
	// "slow link" from "server never answered", and the diagnostic #7498 asks
	// for.
	if stalled.BytesRead <= 0 {
		t.Errorf("BytesRead = %d, want > 0 (some of the body arrived)", stalled.BytesRead)
	}
	if stalled.Op != "GET /stalls" {
		t.Errorf("Op = %q, want %q", stalled.Op, "GET /stalls")
	}
	// It must fail on the stall budget, not on the whole-request ceiling.
	if elapsed > 3*time.Second {
		t.Errorf("took %s to notice a stall with a 150ms budget", elapsed)
	}
}

// A stall must reach the user as a stall. It used to surface as a raw
// "context deadline exceeded ... while reading body" out of the JSON decoder,
// because only errors from http.Client.Do were ever classified.
func TestStalledTransferIsClassifiedAsNetworkError(t *testing.T) {
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "150ms")
	srv := trickleServer(t, 20, 10*time.Millisecond, 4)

	client := &APIClient{BaseURL: srv.URL, HTTPClient: NewStallAwareHTTPClient()}
	var out trickleBody
	err := client.GetJSON(context.Background(), "/stalls", &out)
	if err == nil {
		t.Fatal("expected a stalled transfer to fail")
	}

	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("error is not a *NetworkError: %#v", err)
	}
	if netErr.Kind != KindNetworkStalled {
		t.Errorf("Kind = %v, want KindNetworkStalled", netErr.Kind)
	}
	if got := ExitCodeFor(err); got != ExitNetwork {
		t.Errorf("ExitCodeFor = %d, want %d", got, ExitNetwork)
	}
	msg := FormatError(err, false)
	if !strings.Contains(msg, "MULTICA_HTTP_STALL_TIMEOUT") {
		t.Errorf("user message does not name the knob that fixes it: %q", msg)
	}
	if strings.Contains(msg, "context deadline exceeded") {
		t.Errorf("user message leaked the raw transport error: %q", msg)
	}
}

// wrapBodyRead must not turn every decode failure into a network error: a
// server that answers 200 with malformed JSON is a server bug, and calling it
// a connection problem sends the user to look at their network.
func TestMalformedJSONIsNotReportedAsNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items": [ not json`)
	}))
	t.Cleanup(srv.Close)

	client := &APIClient{BaseURL: srv.URL, HTTPClient: NewStallAwareHTTPClient()}
	var out trickleBody
	err := client.GetJSON(context.Background(), "/broken", &out)
	if err == nil {
		t.Fatal("expected a decode failure")
	}

	var netErr *NetworkError
	if errors.As(err, &netErr) {
		t.Fatalf("malformed JSON was classified as a network error: %v", netErr)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("expected a *json.SyntaxError, got %#v", err)
	}
}

func TestStallTimeoutEnvPrecedence(t *testing.T) {
	t.Run("default when nothing is set", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "")
		t.Setenv("MULTICA_HTTP_TIMEOUT", "")
		if got := StallTimeout(); got != defaultStallTimeout {
			t.Errorf("StallTimeout = %v, want %v", got, defaultStallTimeout)
		}
	})

	// An explicitly set MULTICA_HTTP_TIMEOUT keeps meaning "how long am I
	// willing to wait for this server" on the stall-aware path, rather than
	// silently reverting to a total-elapsed limit.
	t.Run("legacy timeout var still has effect", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "")
		t.Setenv("MULTICA_HTTP_TIMEOUT", "90s")
		if got, want := StallTimeout(), 90*time.Second; got != want {
			t.Errorf("StallTimeout = %v, want %v", got, want)
		}
	})

	t.Run("dedicated var wins", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "5s")
		t.Setenv("MULTICA_HTTP_TIMEOUT", "90s")
		if got, want := StallTimeout(), 5*time.Second; got != want {
			t.Errorf("StallTimeout = %v, want %v", got, want)
		}
	})

	t.Run("bare seconds and invalid values", func(t *testing.T) {
		t.Setenv("MULTICA_HTTP_TIMEOUT", "")
		t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "45")
		if got, want := StallTimeout(), 45*time.Second; got != want {
			t.Errorf("StallTimeout = %v, want %v", got, want)
		}
		for _, bad := range []string{"nonsense", "0", "-5s"} {
			t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", bad)
			if got := StallTimeout(); got != defaultStallTimeout {
				t.Errorf("StallTimeout with %q = %v, want the default %v", bad, got, defaultStallTimeout)
			}
		}
	})
}

// The ceiling is a backstop, so it must never be the thing that fires first —
// including when someone sets a very generous stall budget.
func TestTransferCeilingStaysAboveStallBudget(t *testing.T) {
	t.Setenv("MULTICA_HTTP_TIMEOUT", "")

	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "")
	if got := TransferCeiling(); got != defaultTransferCeiling {
		t.Errorf("TransferCeiling = %v, want %v", got, defaultTransferCeiling)
	}

	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "20m")
	if got, want := TransferCeiling(), 80*time.Minute; got != want {
		t.Errorf("TransferCeiling = %v, want %v", got, want)
	}

	// And it is never absent: an unbounded command is not an improvement over
	// one that gives up too early.
	if TransferCeiling() <= 0 {
		t.Error("TransferCeiling must always be positive")
	}
}

func TestStallAwareContextLeavesTransportDeadlineFirst(t *testing.T) {
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "")
	t.Setenv("MULTICA_HTTP_TIMEOUT", "")

	ctx, cancel := StallAwareContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("StallAwareContext must set a deadline")
	}
	if budget := time.Until(deadline); budget <= TransferCeiling() {
		t.Errorf("context budget %v must exceed the transport ceiling %v", budget, TransferCeiling())
	}
}

// Closing a body early must not leave the guard's timer or context alive.
func TestStallGuardBodyCloseReleasesGuard(t *testing.T) {
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "50ms")
	srv := trickleServer(t, 20, 10*time.Millisecond, 4)

	client := NewStallAwareHTTPClient()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/stalls", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Closing twice must not panic on the already-stopped timer.
	_ = resp.Body.Close()
}

// A body that simply stops mid-JSON is a dropped connection far more often
// than it is a server emitting truncated JSON, so it is classified as a
// network failure. This test pins that choice rather than leaving it to
// whichever error the decoder happens to return.
func TestTruncatedBodyIsReportedAsNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "64")
		fmt.Fprint(w, `{"items": ["abc`)
	}))
	t.Cleanup(srv.Close)

	client := &APIClient{BaseURL: srv.URL, HTTPClient: NewStallAwareHTTPClient()}
	var out trickleBody
	err := client.GetJSON(context.Background(), "/truncated", &out)
	if err == nil {
		t.Fatal("expected a truncated body to fail")
	}

	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("truncated body was not classified as a network error: %#v", err)
	}
	if got := ExitCodeFor(err); got != ExitNetwork {
		t.Errorf("ExitCodeFor = %d, want %d", got, ExitNetwork)
	}
}

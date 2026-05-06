package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func withFastTerminalReportBackoffs(t *testing.T) {
	t.Helper()
	prev := terminalReportBackoffs
	terminalReportBackoffs = []time.Duration{0, 0, 0}
	t.Cleanup(func() { terminalReportBackoffs = prev })
}

func TestReportTerminalTaskResultRetriesTransientTransportError(t *testing.T) {
	withFastTerminalReportBackoffs(t)

	d := &Daemon{}
	var calls int32
	err := d.reportTerminalTaskResult(context.Background(), slog.Default(), "task-1", "complete", func(context.Context) error {
		if atomic.AddInt32(&calls, 1) < 3 {
			return io.EOF
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestReportTerminalTaskResultDoesNotRetryRequestError(t *testing.T) {
	withFastTerminalReportBackoffs(t)

	d := &Daemon{}
	var calls int32
	err := d.reportTerminalTaskResult(context.Background(), slog.Default(), "task-1", "complete", func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		return &requestError{StatusCode: http.StatusInternalServerError, Body: "server error"}
	})

	if err == nil {
		t.Fatal("expected requestError to be returned")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt for semantic HTTP error, got %d", got)
	}
}

func TestReportTerminalTaskResultStopsWhenContextCancelled(t *testing.T) {
	prev := terminalReportBackoffs
	terminalReportBackoffs = []time.Duration{0, time.Hour}
	t.Cleanup(func() { terminalReportBackoffs = prev })

	d := &Daemon{}
	ctx, cancel := context.WithCancel(context.Background())
	var calls int32
	err := d.reportTerminalTaskResult(ctx, slog.Default(), "task-1", "complete", func(context.Context) error {
		atomic.AddInt32(&calls, 1)
		cancel()
		return io.EOF
	})

	if err == nil {
		t.Fatal("expected cancellation to stop retry loop")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 attempt before cancellation, got %d", got)
	}
}

func TestIsTransientTerminalReportError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "connection refused text", err: errors.New("dial tcp 127.0.0.1:8080: connect: connection refused"), want: true},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: true},
		{name: "context cancelled", err: context.Canceled, want: true},
		{name: "request error 404", err: &requestError{StatusCode: http.StatusNotFound}, want: false},
		{name: "plain semantic error", err: errors.New("validation failed"), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isTransientTerminalReportError(tc.err); got != tc.want {
				t.Fatalf("isTransientTerminalReportError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

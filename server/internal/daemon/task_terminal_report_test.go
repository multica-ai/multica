package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestShouldRetryTaskTerminalReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "success", err: nil, want: false},
		{name: "network error", err: errors.New("connection reset"), want: true},
		{name: "bad gateway", err: &requestError{StatusCode: http.StatusBadGateway, Body: "Bad Gateway"}, want: true},
		{name: "request timeout", err: &requestError{StatusCode: http.StatusRequestTimeout, Body: "request timeout"}, want: true},
		{name: "rate limited", err: &requestError{StatusCode: http.StatusTooManyRequests, Body: "too many requests"}, want: true},
		{name: "generic proxy not found", err: &requestError{StatusCode: http.StatusNotFound, Body: "404 page not found"}, want: true},
		{name: "task deleted", err: &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"task not found"}`}, want: false},
		{name: "bad request", err: &requestError{StatusCode: http.StatusBadRequest, Body: `{"error":"invalid request"}`}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldRetryTaskTerminalReport(tt.err); got != tt.want {
				t.Fatalf("shouldRetryTaskTerminalReport(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestReportTaskTerminalWithRetry(t *testing.T) {
	original := taskTerminalReportBackoffs
	taskTerminalReportBackoffs = []time.Duration{0, 0, 0}
	t.Cleanup(func() { taskTerminalReportBackoffs = original })

	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	wantErrors := []error{
		&requestError{StatusCode: http.StatusBadGateway, Body: "Bad Gateway"},
		&requestError{StatusCode: http.StatusNotFound, Body: "404 page not found"},
		nil,
	}
	var gotErrors []error
	err := d.reportTaskTerminalWithRetry(context.Background(), "task-1", "completed", func(context.Context) error {
		next := wantErrors[len(gotErrors)]
		gotErrors = append(gotErrors, next)
		return next
	})
	if err != nil {
		t.Fatalf("reportTaskTerminalWithRetry returned %v", err)
	}
	if !reflect.DeepEqual(gotErrors, wantErrors) {
		t.Fatalf("attempts = %#v, want %#v", gotErrors, wantErrors)
	}
}

func TestReportTaskTerminalWithRetryStopsOnPermanentError(t *testing.T) {
	original := taskTerminalReportBackoffs
	taskTerminalReportBackoffs = []time.Duration{0, 0, 0}
	t.Cleanup(func() { taskTerminalReportBackoffs = original })

	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	want := &requestError{StatusCode: http.StatusNotFound, Body: `{"error":"task not found"}`}
	attempts := 0
	err := d.reportTaskTerminalWithRetry(context.Background(), "task-1", "completed", func(context.Context) error {
		attempts++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestReportTaskTerminalWithRetryReturnsLastTransientError(t *testing.T) {
	original := taskTerminalReportBackoffs
	taskTerminalReportBackoffs = []time.Duration{0, 0}
	t.Cleanup(func() { taskTerminalReportBackoffs = original })

	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	first := errors.New("connection reset")
	last := &requestError{StatusCode: http.StatusBadGateway, Body: "Bad Gateway"}
	errorsByAttempt := []error{first, last}
	attempts := 0
	err := d.reportTaskTerminalWithRetry(context.Background(), "task-1", "completed", func(context.Context) error {
		next := errorsByAttempt[attempts]
		attempts++
		return next
	})
	if !errors.Is(err, last) {
		t.Fatalf("error = %v, want final transient error %v", err, last)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

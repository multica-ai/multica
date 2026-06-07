package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLoggerSummaryOmitsBodies(t *testing.T) {
	t.Setenv("LOG_REQUEST_MODE", "summary")
	t.Setenv("LOG_RESPONSE_DETAIL", "false")

	logs := captureRequestLog(t, `{"email":"person@example.com","title":"hello"}`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":"1"},{"id":"2"}],"token":"secret"}`))
	})

	for _, want := range []string{"method=POST", "path=/api/issues", "status=200", "bytes_written=50", "items:2"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected summary log to contain %q, got %s", want, logs)
		}
	}
	for _, forbidden := range []string{"request_body", "response_body", "person@example.com", "secret"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("expected summary log to omit %q, got %s", forbidden, logs)
		}
	}
}

func TestRequestLoggerEnhancedLogsRedactedRequestInput(t *testing.T) {
	t.Setenv("LOG_REQUEST_MODE", "enhanced")
	t.Setenv("LOG_RESPONSE_DETAIL", "true")

	logs := captureRequestLog(t, `{"email":"person@example.com","title":"hello"}`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"token":"secret"}`))
	})

	for _, want := range []string{"request_body", "response_body", "title:hello", "[REDACTED]"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected enhanced log to contain %q, got %s", want, logs)
		}
	}
	for _, forbidden := range []string{"person@example.com", "token=secret"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("expected enhanced log to redact %q, got %s", forbidden, logs)
		}
	}
}

func captureRequestLog(t *testing.T, body string, handler http.HandlerFunc) string {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id=ws_1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	RequestLogger(handler).ServeHTTP(rr, req)
	return buf.String()
}

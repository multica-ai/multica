package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// withCapturedLogs swaps the default slog logger for one that writes to buf,
// then restores it on cleanup. Returns the buffer so tests can inspect what
// RequestLogger emitted.
//
// Uses a shared mutex because t.Parallel tests would otherwise race on the
// global slog.Default — tests in this file intentionally do NOT run in
// parallel for that reason.
var defaultLoggerMu sync.Mutex

func withCapturedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	defaultLoggerMu.Lock()
	buf := &bytes.Buffer{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(orig)
		defaultLoggerMu.Unlock()
	})
	return buf
}

func runRequestLogger(t *testing.T, status int, body string) *bytes.Buffer {
	t.Helper()
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/heartbeat", nil).
		WithContext(context.Background())
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return logs
}

// requireLogLevel asserts that the captured output contains exactly the
// expected slog level prefix and not any of the disallowed ones.
func requireLogLevel(t *testing.T, logs *bytes.Buffer, want string, disallowed ...string) {
	t.Helper()
	out := logs.String()
	if !strings.Contains(out, "level="+want) {
		t.Fatalf("expected level=%s in logs, got:\n%s", want, out)
	}
	for _, dis := range disallowed {
		if strings.Contains(out, "level="+dis) {
			t.Fatalf("did not expect level=%s in logs, got:\n%s", dis, out)
		}
	}
}

func TestRequestLogger_RuntimeNotFound404DowngradesToInfo(t *testing.T) {
	// The whole reason this middleware change exists: a flood of WRN lines
	// after a runtime is deleted (issue #2391). The daemon catches the same
	// body and self-heals, so the line is signal-not-noise.
	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"runtime not found"}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_TaskNotFound404DowngradesToInfo(t *testing.T) {
	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"task not found"}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_GenericNotFound404KeepsWarn(t *testing.T) {
	// A 404 with an unfamiliar body is still a real 404 — most likely a
	// daemon hitting a wrong path, which is what Warn is for. We do NOT
	// want to downgrade these blindly.
	logs := runRequestLogger(t, http.StatusNotFound, `{"error":"not found"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestRequestLogger_400StaysWarn(t *testing.T) {
	logs := runRequestLogger(t, http.StatusBadRequest, `{"error":"bad input"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestRequestLogger_500StaysError(t *testing.T) {
	logs := runRequestLogger(t, http.StatusInternalServerError, `{"error":"boom"}`)
	requireLogLevel(t, logs, "ERROR", "WARN", "INFO")
}

func TestRequestLogger_200StaysInfo(t *testing.T) {
	logs := runRequestLogger(t, http.StatusOK, `{"ok":true}`)
	requireLogLevel(t, logs, "INFO", "WARN", "ERROR")
}

func TestRequestLogger_HealthEndpointIsSkipped(t *testing.T) {
	logs := withCapturedLogs(t)
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if logs.Len() != 0 {
		t.Fatalf("/health should not be logged, got:\n%s", logs.String())
	}
}

func TestRequestLogger_BodyStillReachesClient(t *testing.T) {
	// The body capture is implemented via Tee, which must mirror writes
	// rather than swallow them. Regress-protect: assert the response writer
	// still gets the full body.
	rec := httptest.NewRecorder()
	handler := RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"runtime not found"}`))
	}))
	_ = withCapturedLogs(t)
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/daemon/heartbeat", nil))
	if got := rec.Body.String(); got != `{"error":"runtime not found"}` {
		t.Fatalf("response body lost or mutated: got %q", got)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRequestLogger_LargeBodyBeyondCaptureLimit(t *testing.T) {
	// If the soft-404 marker only appears beyond the capture limit we
	// intentionally keep Warn — capturing arbitrary-size bodies is the
	// memory blowup we are guarding against. This test pins that
	// trade-off.
	prefix := strings.Repeat("x", softNotFoundBodyCaptureLimit+8)
	logs := runRequestLogger(t, http.StatusNotFound, prefix+`{"error":"runtime not found"}`)
	requireLogLevel(t, logs, "WARN", "INFO", "ERROR")
}

func TestIsSoftNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body string
		want bool
	}{
		{`{"error":"runtime not found"}`, true},
		{`{"error":"task not found"}`, true},
		{`{"error":"Runtime Not Found"}`, true},
		{`{"error":"not found"}`, false},
		{`{"error":"workspace not found"}`, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSoftNotFound([]byte(tc.body)); got != tc.want {
			t.Errorf("isSoftNotFound(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

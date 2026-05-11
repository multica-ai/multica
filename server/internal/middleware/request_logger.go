package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// boundedBuffer captures up to Cap bytes from a stream then silently drops the
// rest. Used by RequestLogger so a large response body cannot blow up logger
// memory while we mirror just enough bytes to classify the response.
type boundedBuffer struct {
	buf bytes.Buffer
	cap int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remain := b.cap - b.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		b.buf.Write(p[:remain])
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }

// softNotFoundBodyCaptureLimit is the maximum number of body bytes the
// request logger inspects to decide whether a 404 is an expected stale-state
// signal (runtime/task deleted server-side). The JSON error envelope is small
// — 256 bytes is enough to see the "error" field — and the cap means an
// unbounded handler body cannot blow up logger memory.
const softNotFoundBodyCaptureLimit = 256

// softNotFoundMarkers are 404 response bodies the daemon emits routinely as
// part of normal lifecycle events: a runtime deleted from the UI, a task GC'd
// after an issue was removed, etc. Logging these at Warn turned production
// stderr into a flood whenever a runtime was deleted (see issue #2391). They
// stay machine-recognizable at Info, while genuine 4xx (wrong path, bad
// auth, real bugs) keep Warn.
var softNotFoundMarkers = []string{
	"runtime not found",
	"task not found",
}

// RequestLogger is a structured HTTP request logger using slog.
// It replaces Chi's built-in chimw.Logger with colored, structured output.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip the hot liveness endpoint to keep logs readable.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		// Capture a small body prefix so 404s can be classified by content.
		// chimw.WrapResponseWriter exposes Tee for exactly this — the body
		// keeps flowing to the client; we mirror up to N bytes for inspection.
		bodyPrefix := &boundedBuffer{cap: softNotFoundBodyCaptureLimit}
		ww.Tee(bodyPrefix)

		next.ServeHTTP(ww, r)

		duration := time.Since(start)
		status := ww.Status()

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration", duration.Round(time.Microsecond).String(),
		}
		if rid := chimw.GetReqID(r.Context()); rid != "" {
			attrs = append(attrs, "request_id", rid)
		}
		if uid := r.Header.Get("X-User-ID"); uid != "" {
			attrs = append(attrs, "user_id", uid)
		}
		if platform, version, os := ClientMetadataFromContext(r.Context()); platform != "" || version != "" || os != "" {
			if platform != "" {
				attrs = append(attrs, "client_platform", platform)
			}
			if version != "" {
				attrs = append(attrs, "client_version", version)
			}
			if os != "" {
				attrs = append(attrs, "client_os", os)
			}
		}

		switch {
		case status >= 500:
			slog.Error("http request", attrs...)
		case status == http.StatusNotFound && isSoftNotFound(bodyPrefix.Bytes()):
			// Lifecycle 404 — runtime/task was deleted server-side. The daemon
			// catches this exact body and triggers its own self-heal, so it is
			// neither noise nor a bug; logging at Info keeps the signal in
			// structured logs without flooding the warn channel.
			slog.Info("http request", attrs...)
		case status >= 400:
			slog.Warn("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	})
}

// isSoftNotFound reports whether the captured response body matches one of
// the expected stale-state 404 signals listed in softNotFoundMarkers.
func isSoftNotFound(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	lower := strings.ToLower(string(body))
	for _, marker := range softNotFoundMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/multica-ai/multica/server/internal/logger"
)

const logBodyLimit = 16 * 1024

// RequestLogger is a structured HTTP request logger using slog.
// It replaces Chi's built-in chimw.Logger with colored, structured output.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip noisy endpoints.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		cfg := logger.ConfigFromEnv()
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		requestAttrs := requestLogAttrs(r, cfg)

		if isWebSocketUpgrade(r) {
			next.ServeHTTP(ww, r)
			emitRequestLog(r, ww.Status(), ww.BytesWritten(), time.Since(start), requestAttrs, nil)
			return
		}

		rw := newResponseLogWriter(ww)
		next.ServeHTTP(rw, r)

		emitRequestLog(r, rw.Status(), rw.BytesWritten(), time.Since(start), requestAttrs, responseLogAttrs(rw, cfg))
	})
}

func emitRequestLog(r *http.Request, status, bytesWritten int, duration time.Duration, requestAttrs, responseAttrs []any) {
	attrs := []any{
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"duration", duration.Round(time.Microsecond).String(),
		"bytes_written", bytesWritten,
	}
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		attrs = append(attrs, "request_id", rid)
	}
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		attrs = append(attrs, "user_id", uid)
	}
	attrs = append(attrs, requestAttrs...)
	attrs = append(attrs, responseAttrs...)

	switch {
	case status >= 500:
		slog.Error("http request", attrs...)
	case status >= 400:
		slog.Warn("http request", attrs...)
	default:
		slog.Info("http request", attrs...)
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

type responseLogWriter struct {
	chimw.WrapResponseWriter
	body bytes.Buffer
}

func newResponseLogWriter(ww chimw.WrapResponseWriter) *responseLogWriter {
	return &responseLogWriter{WrapResponseWriter: ww}
}

func (w *responseLogWriter) Write(p []byte) (int, error) {
	if w.body.Len() < logBodyLimit {
		remaining := logBodyLimit - w.body.Len()
		if len(p) > remaining {
			w.body.Write(p[:remaining])
		} else {
			w.body.Write(p)
		}
	}
	return w.WrapResponseWriter.Write(p)
}

func requestLogAttrs(r *http.Request, cfg logger.Config) []any {
	attrs := make([]any, 0, 8)
	if cfg.RequestMode == logger.RequestLogSummary {
		return attrs
	}

	if len(r.URL.Query()) > 0 {
		attrs = append(attrs, "query", logger.RedactValue(queryValues(r)))
	}
	if cfg.RequestMode == logger.RequestLogFull {
		attrs = append(attrs,
			"remote_addr", r.RemoteAddr,
			"user_agent", logger.RedactString(r.UserAgent()),
			"content_length", r.ContentLength,
		)
	}
	if body, ok := readRequestBodyForLog(r); ok {
		attrs = append(attrs, "request_body", logger.RedactValue(body))
	}
	return attrs
}

func responseLogAttrs(w *responseLogWriter, cfg logger.Config) []any {
	attrs := []any{"response_summary", responseSummary(w)}
	if !cfg.ResponseDetail || w.body.Len() == 0 {
		return attrs
	}

	content := strings.TrimSpace(w.body.String())
	if body, ok := decodeJSONForLog([]byte(content)); ok {
		attrs = append(attrs, "response_body", logger.RedactValue(body))
	} else {
		attrs = append(attrs, "response_body", logger.RedactString(content))
	}
	if w.BytesWritten() > logBodyLimit {
		attrs = append(attrs, "response_body_truncated", true)
	}
	return attrs
}

func responseSummary(w *responseLogWriter) map[string]any {
	summary := map[string]any{
		"bytes": w.BytesWritten(),
	}
	if count, ok := responseItemCount(w.body.Bytes()); ok {
		summary["items"] = count
	}
	return summary
}

func queryValues(r *http.Request) map[string]any {
	out := make(map[string]any, len(r.URL.Query()))
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			out[key] = values[0]
		} else {
			items := make([]any, 0, len(values))
			for _, value := range values {
				items = append(items, value)
			}
			out[key] = items
		}
	}
	return out
}

func readRequestBodyForLog(r *http.Request) (any, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return nil, false
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, logBodyLimit+1))
	if err != nil {
		return map[string]any{"read_error": err.Error()}, true
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > logBodyLimit {
		body = body[:logBodyLimit]
	}
	if decoded, ok := decodeJSONForLog(body); ok {
		return decoded, true
	}
	return strings.TrimSpace(string(body)), true
}

func decodeJSONForLog(body []byte) (any, bool) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func responseItemCount(body []byte) (int, bool) {
	decoded, ok := decodeJSONForLog(body)
	if !ok {
		return 0, false
	}
	switch value := decoded.(type) {
	case []any:
		return len(value), true
	case map[string]any:
		for _, key := range []string{"items", "data", "results"} {
			if items, ok := value[key].([]any); ok {
				return len(items), true
			}
		}
	}
	return 0, false
}

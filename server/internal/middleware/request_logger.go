package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

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
		case status >= 400:
			slog.Warn("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	})
}

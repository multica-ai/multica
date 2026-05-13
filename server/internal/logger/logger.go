package logger

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/lmittmann/tint"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// isTerminal reports whether the given file descriptor is connected to a
// terminal. Used to suppress ANSI color escapes when stderr is redirected
// to a file (e.g. daemon.log), so log files stay clean.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Init initializes the global slog logger. Colors are enabled when stderr
// is a terminal and disabled otherwise. Reads LOG_LEVEL env var (debug,
// info, warn, error). Default: debug.
func Init() {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      level,
		TimeFormat: "15:04:05.000",
		NoColor:    !isTerminal(os.Stderr),
	})
	slog.SetDefault(slog.New(handler))
}

// NewLogger creates a named slog logger. Colors follow the same
// TTY-detection rule as Init. Useful for standalone processes (daemon,
// migrate) that want a component prefix.
func NewLogger(component string) *slog.Logger {
	level := parseLevel(os.Getenv("LOG_LEVEL"))
	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      level,
		TimeFormat: "15:04:05.000",
		NoColor:    !isTerminal(os.Stderr),
	})
	return slog.New(handler).With("component", component)
}

// RequestAttrs extracts request_id, user_id, and X-Client-* metadata from
// an HTTP request for use in handler-level structured logging. Mirrors the
// global request logger so handler logs end up with the same observability
// dimensions as the access log.
func RequestAttrs(r *http.Request) []any {
	attrs := make([]any, 0, 10)
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		attrs = append(attrs, "request_id", rid)
	}
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		attrs = append(attrs, "user_id", uid)
	}
	platform, version, os := middleware.ClientMetadataFromContext(r.Context())
	if platform != "" {
		attrs = append(attrs, "client_platform", platform)
	}
	if version != "" {
		attrs = append(attrs, "client_version", version)
	}
	if os != "" {
		attrs = append(attrs, "client_os", os)
	}
	return attrs
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

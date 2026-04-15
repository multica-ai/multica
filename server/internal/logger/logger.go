package logger

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/lmittmann/tint"
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

// RequestAttrs extracts request_id and user_id from an HTTP request
// for use in handler-level structured logging.
func RequestAttrs(r *http.Request) []any {
	attrs := make([]any, 0, 4)
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		attrs = append(attrs, "request_id", rid)
	}
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		attrs = append(attrs, "user_id", uid)
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

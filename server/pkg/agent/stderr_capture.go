package agent

import (
	"log/slog"
	"strings"
	"sync"
)

const defaultCapturedStderrLines = 40

// logWriter adapts a *slog.Logger to an io.Writer and keeps a short tail of
// recent stderr lines for post-run diagnostics.
type logWriter struct {
	logger   *slog.Logger
	prefix   string
	maxLines int

	mu    sync.Mutex
	lines []string
}

func newLogWriter(logger *slog.Logger, prefix string) *logWriter {
	return &logWriter{
		logger:   logger,
		prefix:   prefix,
		maxLines: defaultCapturedStderrLines,
	}
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" {
		return len(p), nil
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		w.logger.Debug(w.prefix + line)
		w.capture(line)
	}

	return len(p), nil
}

func (w *logWriter) capture(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lines = append(w.lines, line)
	if len(w.lines) > w.maxLines {
		w.lines = append([]string(nil), w.lines[len(w.lines)-w.maxLines:]...)
	}
}

func (w *logWriter) Tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return strings.Join(w.lines, "\n")
}

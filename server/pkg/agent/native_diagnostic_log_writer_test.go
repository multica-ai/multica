package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestSanitizedLogWriterRedactsCredentialSplitAcrossWrites(t *testing.T) {
	const prefix = "[fixture:stderr] "
	const canary = "split-native-diagnostic-canary-84f1"
	capture := &nativeDiagnosticMessageCapture{}
	writer := newSanitizedLogWriter(
		slog.New(capture),
		prefix,
		[]string{"PROVIDER_API_KEY=" + canary},
	)

	split := len(canary) / 2
	_, _ = writer.Write([]byte(canary[:split]))
	if got := capture.reconstructed(prefix); got != "" {
		t.Fatal("sanitized log writer emitted an incomplete diagnostic")
	}
	_, _ = writer.Write([]byte(canary[split:] + "\n"))

	if strings.Contains(capture.reconstructed(prefix), canary) {
		t.Fatal("sanitized log writer leaked a credential split across writes")
	}
}

func TestSanitizedLogWriterOmitsOversizedDiagnosticWithoutRawContent(t *testing.T) {
	const prefix = "[fixture:stderr] "
	const canary = "oversized-native-diagnostic-canary-19c2"
	capture := &nativeDiagnosticMessageCapture{}
	writer := newSanitizedLogWriter(
		slog.New(capture),
		prefix,
		[]string{"PROVIDER_API_KEY=" + canary},
	)

	oversized := strings.Repeat("x", 128<<10) + canary + "\n"
	_, _ = writer.Write([]byte(oversized))
	reconstructed := capture.reconstructed(prefix)
	if strings.Contains(reconstructed, canary) {
		t.Fatal("sanitized log writer leaked raw content from an oversized diagnostic")
	}
	if reconstructed != sanitizedLogOmissionMarker {
		t.Fatalf("sanitized log writer emitted %q instead of the omission marker", reconstructed)
	}
	if len(reconstructed) > 1024 {
		t.Fatal("sanitized log writer retained an unbounded oversized diagnostic")
	}
}

func TestSanitizedLogWriterFlushesFinalUnterminatedDiagnostic(t *testing.T) {
	const prefix = "[fixture:stderr] "
	const canary = "unterminated-native-diagnostic-canary-5bd7"
	capture := &nativeDiagnosticMessageCapture{}
	writer := newSanitizedLogWriter(
		slog.New(capture),
		prefix,
		[]string{"PROVIDER_API_KEY=" + canary},
	)

	_, _ = writer.Write([]byte("provider failure " + canary))
	writer.Flush()

	reconstructed := capture.reconstructed(prefix)
	if reconstructed == "" {
		t.Fatal("sanitized log writer dropped a bounded final diagnostic")
	}
	if strings.Contains(reconstructed, canary) {
		t.Fatal("sanitized log writer flush leaked an unterminated credential")
	}
}

type nativeDiagnosticMessageCapture struct {
	mu       sync.Mutex
	messages []string
}

func (*nativeDiagnosticMessageCapture) Enabled(context.Context, slog.Level) bool {
	return true
}

func (c *nativeDiagnosticMessageCapture) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, record.Message)
	return nil
}

func (c *nativeDiagnosticMessageCapture) WithAttrs([]slog.Attr) slog.Handler {
	return c
}

func (c *nativeDiagnosticMessageCapture) WithGroup(string) slog.Handler {
	return c
}

func (c *nativeDiagnosticMessageCapture) reconstructed(prefix string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result strings.Builder
	for _, message := range c.messages {
		result.WriteString(strings.TrimPrefix(message, prefix))
	}
	return result.String()
}

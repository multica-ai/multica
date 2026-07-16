package agent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/pkg/redact"
)

const piDiagnosticTailBytes = 16 * 1024

type piDiagnosticWriter struct {
	safe *secretSafeDiagnosticWriter
	tail *ringBuffer
}

func newPiDiagnosticWriter(logger *slog.Logger) *piDiagnosticWriter {
	tail := newRingBuffer(piDiagnosticTailBytes)
	return &piDiagnosticWriter{
		safe: newSecretSafeDiagnosticWriter(newLogWriter(logger, "[pi:stderr] "), tail),
		tail: tail,
	}
}

func (w *piDiagnosticWriter) Write(p []byte) (int, error) {
	return w.safe.Write(p)
}

func (w *piDiagnosticWriter) safeError(exitErr error) string {
	w.safe.Flush()

	if category := piProviderErrorCategory(w.tail.Snapshot()); category != "" {
		return "pi provider error: " + category
	}
	return fmt.Sprintf("pi exited with error: %v", exitErr)
}

func safePiProviderEventError(message string) string {
	safe := redact.Text(message)
	if category := piProviderErrorCategory(safe); category != "" {
		return "pi provider error: " + category
	}
	return safe
}

func piProviderErrorCategory(message string) string {
	diagnostic := strings.ToLower(message)
	switch {
	case containsAny(diagnostic, "usage limit", "subscription", "insufficient_quota", "quota exceeded", "credit balance"):
		return "subscription or quota limit"
	case containsAny(diagnostic, "status 401", "http 401", "status 403", "http 403", "unauthorized", "invalid_grant", "authentication failed", "token expired"):
		return "authentication rejected"
	case containsAny(diagnostic, "status 429", "http 429", "too many requests", "rate limit", "rate_limit"):
		return "rate limited"
	case containsAny(diagnostic, "connection refused", "connection reset", "network is unreachable", "no such host", "temporary failure in name resolution", "tls handshake timeout"):
		return "network unavailable"
	}
	return ""
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

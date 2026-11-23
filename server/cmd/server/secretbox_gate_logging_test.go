package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// captureDefaultLogger swaps the package-level slog default for one writing
// into buf and returns a restore func, so the assertions see exactly what the
// gate helper emitted through slog.Info/slog.Warn.
func captureDefaultLogger(buf *bytes.Buffer) func() {
	previous := slog.Default()
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	return func() { slog.SetDefault(previous) }
}

func TestLogSecretboxGateDisabledDistinguishesSetButInvalidFromNotSet(t *testing.T) {
	const envVar = "MULTICA_TELEGRAM_SECRET_KEY"
	const gateErr = "secretbox: MULTICA_TELEGRAM_SECRET_KEY decodes to 13 bytes, expected 32"

	t.Setenv(envVar, "not-a-valid-key")

	var logs bytes.Buffer
	restore := captureDefaultLogger(&logs)
	defer restore()

	logSecretboxGateDisabled("telegram integration", envVar, errors.New(gateErr))

	out := logs.String()
	if !strings.Contains(out, "WARN") {
		t.Fatalf("a set-but-invalid key must log at WARN, got:\n%s", out)
	}
	if !strings.Contains(out, "is set but invalid") {
		t.Fatalf("the message must say the variable is set but invalid, got:\n%s", out)
	}
	if !strings.Contains(out, gateErr) {
		t.Fatalf("the decode reason must be surfaced, got:\n%s", out)
	}
	if strings.Contains(out, "not set") {
		t.Fatalf("a set-but-invalid key must not be reported as \"not set\", got:\n%s", out)
	}
}

func TestLogSecretboxGateDisabledKeepsNotSetAtInfo(t *testing.T) {
	const envVar = "MULTICA_TELEGRAM_SECRET_KEY"
	const gateErr = "secretbox: MULTICA_TELEGRAM_SECRET_KEY is not set"

	t.Setenv(envVar, "")

	var logs bytes.Buffer
	restore := captureDefaultLogger(&logs)
	defer restore()

	logSecretboxGateDisabled("telegram integration", envVar, errors.New(gateErr))

	out := logs.String()
	if !strings.Contains(out, "INFO") {
		t.Fatalf("an intentionally unset variable stays a normal INFO line, got:\n%s", out)
	}
	if !strings.Contains(out, "not set") {
		t.Fatalf("the message must say the variable is not set, got:\n%s", out)
	}
	if strings.Contains(out, "WARN") {
		t.Fatalf("an unset variable is not an operator error and must not warn, got:\n%s", out)
	}
}

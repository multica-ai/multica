package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestOpenclawIsolatedModeOptIn(t *testing.T) {
	var logs bytes.Buffer
	mode, pin := decodeOpenclawRuntimeConfig(json.RawMessage(`{"mode":"isolated-local","model_override":"codex-lb/gpt-6-astra","gateway":{"host":"unchanged","token":"fixture-only"}}`), slog.New(slog.NewTextHandler(&logs, nil)))
	if mode != "isolated-local" || !pin.IsZero() || strings.Contains(logs.String(), "unrecognized") {
		t.Fatalf("mode=%q pin=%v logs=%s", mode, pin, logs.String())
	}
	for _, raw := range []string{`{}`, `{"mode":"local"}`, `{"mode":"gateway"}`} {
		m, _ := decodeOpenclawRuntimeConfig(json.RawMessage(raw), quietLogger())
		if m == "isolated-local" {
			t.Fatal("isolation must be opt in")
		}
	}
}

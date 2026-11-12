package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestOpenclawMalformedProtocolFailsClosed(t *testing.T) {
	for _, raw := range []string{
		`{"runId":"r","status":"accepted"}`,
		`{"runId":"r","status":"ok","result":null}`,
		`{"runId":"r","status":"error","result":{"meta":{"error":{}}}}`,
		`{"meta":{"error":{"message":"failed"}}}`,
		`{"meta":{"error":{}}}`,
		`{"type":"unknown_future_event"}`,
		`{"payloads":`,
		"log preamble\n{\n  \"runId\": \"r\",\n  \"status\": \"accepted\"\n}",
	} {
		t.Run(raw, func(t *testing.T) {
			b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
			got := b.processOutput(strings.NewReader(raw), make(chan Message, 20))
			if got.status != "failed" || got.errMsg == "" || got.output != "" || got.sessionID != "" || got.usage.InputTokens != 0 {
				t.Fatalf("malformed protocol became result: %+v", got)
			}
		})
	}
}

func TestOpenclawPlainTextCompatibility(t *testing.T) {
	b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
	got := b.processOutput(strings.NewReader("Normal reply with {braces}.\nSecond line."), make(chan Message, 20))
	if got.status != "completed" || got.output != "Normal reply with {braces}.\nSecond line." || got.errMsg != "" {
		t.Fatalf("%+v", got)
	}
}

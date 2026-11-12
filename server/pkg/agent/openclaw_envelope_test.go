package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestOpenclawGatewayEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, raw, status, text, session string
		input                            int64
	}{
		{"success", `{"runId":"run","status":"ok","result":{"payloads":[{"text":"OK"}],"meta":{"agentMeta":{"sessionId":"session","model":"model","usage":{"input":12,"output":3}}}}}`, "completed", "OK", "session", 12},
		{"error", `{"runId":"run","status":"error","error":{"message":"gateway failed"}}`, "failed", "", "", 0},
		{"timeout", `{"runId":"run","status":"timeout"}`, "failed", "", "", 0},
		{"nested_error", `{"runId":"run","status":"ok","result":{"payloads":[],"meta":{"error":{"message":"provider failed"}}}}`, "failed", "", "", 0},
		{"reasoning_hidden", `{"runId":"run","status":"ok","result":{"payloads":[{"text":"private reasoning","isReasoning":true},{"text":"progress","isCommentary":true},{"text":"OK"}],"meta":{}}}`, "completed", "OK", "", 0},
		{"error_payload", `{"runId":"run","status":"ok","result":{"payloads":[{"text":"provider failed","isError":true}],"meta":{}}}`, "failed", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &openclawBackend{cfg: Config{Logger: slog.Default()}}
			ch := make(chan Message, 20)
			got := b.processOutput(strings.NewReader(tc.raw), ch)
			if got.status != tc.status || got.output != tc.text || got.sessionID != tc.session || got.usage.InputTokens != tc.input {
				t.Fatalf("got %+v", got)
			}
			if tc.status == "failed" && got.errMsg == "" {
				t.Fatal("missing failure diagnostic")
			}
		})
	}
}

func TestOpenclawRejectUnrelatedEnvelope(t *testing.T) {
	for _, raw := range []string{`{"result":{"payloads":[{"text":"unrelated"}]}}`, `{"runId":"r","status":"accepted"}`, `{"runId":"r","status":"ok","result":null}`} {
		if _, ok := tryParseOpenclawResult(raw); ok {
			t.Fatalf("accepted unrelated/incomplete envelope: %s", raw)
		}
	}
}

package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexTokenCountLine(input, window int64) string {
	return fmt.Sprintf(`{"timestamp":"t","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":999999},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":1,"output_tokens":5},"model_context_window":%d}}}`, input, window)
}

func writeCodexRollout(t *testing.T, codexHome, sessionID, name string, lines ...string) {
	t.Helper()
	path := filepath.Join(codexHome, "sessions", "2026", "11", "27", "rollout-2026-11-27T00-00-00-"+sessionID+name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexResumeContextUsage(t *testing.T) {
	t.Parallel()
	const session = "01a20e57-0000-7000-8000-000000000001"
	userMessage := `{"type":"response_item","payload":{"type":"message","role":"user","content":"` + strings.Repeat("x", 200<<10) + `"}}`
	rateLimitsOnly := `{"type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{}}}`
	compacted := `{"type":"compacted","payload":{"message":"summary","replacement_history":[]}}`

	tests := []struct {
		name   string
		lines  []string
		file   string
		want   CodexContextUsage
		wantOK bool
	}{
		{
			name:   "latest token_count wins, across chunk boundaries",
			lines:  []string{codexTokenCountLine(1000, 272000), userMessage, codexTokenCountLine(210000, 272000), userMessage, ""},
			want:   CodexContextUsage{InputTokens: 210000, ContextWindow: 272000},
			wantOK: true,
		},
		{
			name:   "skips token_count events without info",
			lines:  []string{codexTokenCountLine(50000, 272000), rateLimitsOnly},
			want:   CodexContextUsage{InputTokens: 50000, ContextWindow: 272000},
			wantOK: true,
		},
		{
			name:   "token_count after a compaction counts",
			lines:  []string{codexTokenCountLine(260000, 272000), compacted, codexTokenCountLine(205000, 272000)},
			want:   CodexContextUsage{InputTokens: 205000, ContextWindow: 272000},
			wantOK: true,
		},
		{
			name:   "a compaction after the last token_count is unknown",
			lines:  []string{codexTokenCountLine(260000, 272000), compacted},
			wantOK: false,
		},
		{
			name:   "no token_count",
			lines:  []string{userMessage},
			wantOK: false,
		},
		{
			name:   "half-written last line is ignored",
			lines:  []string{codexTokenCountLine(70000, 272000), `{"type":"event_msg","payload":{"type":"token_co`},
			want:   CodexContextUsage{InputTokens: 70000, ContextWindow: 272000},
			wantOK: true,
		},
		{
			name:   "compressed rollouts are not read",
			file:   ".jsonl.zst",
			lines:  []string{codexTokenCountLine(260000, 272000)},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codexHome := t.TempDir()
			file := tt.file
			if file == "" {
				file = ".jsonl"
			}
			writeCodexRollout(t, codexHome, session, file, tt.lines...)
			got, ok := CodexResumeContextUsage(codexHome, session)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("CodexResumeContextUsage = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}

	if _, ok := CodexResumeContextUsage(t.TempDir(), session); ok {
		t.Fatal("missing rollout reported usage")
	}
}

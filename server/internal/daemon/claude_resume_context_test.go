package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestGateClaudeResumeToContextSize(t *testing.T) {
	t.Parallel()
	const large = `{"type":"assistant","message":{"usage":{"input_tokens":2001,"cache_read_input_tokens":280000,"cache_creation_input_tokens":18000}}}`
	const boundary = `{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":280000,"cache_creation_input_tokens":18000,"output_tokens":5000}}}`
	const compacted = `{"type":"assistant","message":{"usage":{"input_tokens":2000,"cache_read_input_tokens":10000,"cache_creation_input_tokens":5000}}}`
	for _, tc := range []struct {
		name, transcript, provider string
		limit                      int64 // 0 = default limit
		disabled                   bool
		drop                       bool
		wantTokens                 int64 // logged context_tokens when dropped; 0 = 300001
	}{
		{name: "over limit", transcript: large, drop: true},
		{name: "exact limit excludes output", transcript: boundary},
		{name: "below limit after compaction", transcript: large + "\n" + `{"type":"system","subtype":"compact_boundary"}` + "\n" + compacted},
		{name: "latest large usage", transcript: compacted + "\n" + large + "\n" + `{"type":"user","message":"next turn"}`, drop: true},
		{name: "missing file"},
		{name: "malformed tail", transcript: large + "\n" + `{"type":`},
		{name: "malformed usage", transcript: large + "\n" + `{"type":"assistant","message":{"usage":{"input_tokens":"bad"}}}`},
		{name: "missing latest usage", transcript: large + "\n" + `{"type":"assistant","message":{}}`},
		{name: "empty latest usage", transcript: large + "\n" + `{"type":"assistant","message":{"usage":{}}}`},
		{name: "no assistant", transcript: `{"type":"user","message":{}}`},
		{name: "different provider", transcript: large, provider: "codex"},
		{name: "lower configured limit", transcript: compacted, limit: 10_000, drop: true, wantTokens: 17_000},
		{name: "disabled", transcript: large, disabled: true},
		// A large record after the assistant forces the reverse reader across
		// multiple chunk boundaries, including a split assistant line.
		{name: "chunk boundaries", transcript: large + "\n" + `{"type":"user","message":"` + strings.Repeat("x", 3*64*1024) + `"}` + "\n", drop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configDir, workDir := t.TempDir(), t.TempDir()
			const sessionID = "prior-session"
			path := filepath.Join(configDir, "projects", encodeClaudeCwd(workDir), sessionID+".jsonl")
			if tc.transcript != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.transcript), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			provider := tc.provider
			if provider == "" {
				provider = "claude"
			}
			task := Task{PriorSessionID: sessionID, IssueID: "issue-1"}
			taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: true}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			limit := int64(DefaultClaudeResumeContextLimit)
			if tc.limit != 0 {
				limit = tc.limit
			}
			if tc.disabled {
				limit = 0
			}
			gateClaudeResumeToContextSize(&task, &taskCtx, provider, workDir, map[string]string{"CLAUDE_CONFIG_DIR": configDir}, limit, logger)
			if (task.PriorSessionID == "") != tc.drop || task.PriorSessionResumeUnavailable != tc.drop || taskCtx.PriorSessionResumeUnavailable != tc.drop || taskCtx.PriorSessionResumed == tc.drop {
				t.Fatalf("drop=%v: task=%+v, context=%+v", tc.drop, task, taskCtx)
			}
			prompt := BuildPrompt(task, provider)
			if strings.Contains(prompt, execenv.SessionContinuityNoticeIssue) != tc.drop {
				t.Fatalf("cold-start continuity notice does not match drop=%v", tc.drop)
			}
			if tc.drop {
				var entry struct {
					Level     string `json:"level"`
					SessionID string `json:"session_id"`
					Tokens    int64  `json:"context_tokens"`
				}
				if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
					t.Fatal(err)
				}
				wantTokens := tc.wantTokens
				if wantTokens == 0 {
					wantTokens = 300001
				}
				if entry.Level != "INFO" || entry.SessionID != sessionID || entry.Tokens != wantTokens {
					t.Fatalf("incorrect diagnostic: %+v", entry)
				}
			} else if logs.Len() != 0 {
				t.Fatalf("unexpected reset log: %s", logs.String())
			}
		})
	}
}

func TestLoadConfig_ClaudeResumeContextLimit(t *testing.T) {
	stageFakeAgent(t)
	redirectTestHome(t, t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	base := Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()}

	for _, tc := range []struct {
		env     string
		want    int64
		wantErr string
	}{
		{env: "", want: DefaultClaudeResumeContextLimit},
		{env: "500000", want: 500_000},
		{env: "0", want: 0},
		{env: "-1", wantErr: "must be 0 (disabled) or a positive token count"},
		{env: "lots", wantErr: "invalid integer"},
	} {
		t.Run("env="+tc.env, func(t *testing.T) {
			t.Setenv("MULTICA_CLAUDE_RESUME_CONTEXT_LIMIT", tc.env)
			cfg, err := LoadConfig(base)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadConfig error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ClaudeResumeContextLimit != tc.want {
				t.Fatalf("ClaudeResumeContextLimit = %d, want %d", cfg.ClaudeResumeContextLimit, tc.want)
			}
		})
	}
}

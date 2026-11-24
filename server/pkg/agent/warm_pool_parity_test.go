package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The warm pool only changes where a turn's process comes from and whether it
// is parked afterwards. These tests run one conversation with the pool
// disabled (MULTICA_WARM_POOL=0 or a custom runtime profile) and enabled
// (a pool miss on the first turn, pool hits after it) and hold every run to
// the same expectations.

type parityTurn struct {
	taskID  string
	prompt  string
	resume  bool
	wantErr string
}

var parityConversation = []parityTurn{
	{taskID: "task-1", prompt: "first turn"},
	{taskID: "task-2", prompt: "second turn", resume: true},
	{taskID: "task-3", prompt: "please fail", resume: true, wantErr: "rate limit exceeded"},
}

type parityOutcome struct {
	result           Result
	text             string
	statusSessionIDs []string
}

func collectParityOutcome(t *testing.T, sess *Session) parityOutcome {
	t.Helper()
	var out parityOutcome
	var text strings.Builder
	timeout := time.After(20 * time.Second)
	for {
		select {
		case msg, ok := <-sess.Messages:
			if !ok {
				select {
				case out.result = <-sess.Result:
				case <-timeout:
					t.Fatal("timed out waiting for result")
				}
				out.text = text.String()
				return out
			}
			switch msg.Type {
			case MessageText:
				text.WriteString(msg.Content)
			case MessageStatus:
				if msg.SessionID != "" {
					out.statusSessionIDs = append(out.statusSessionIDs, msg.SessionID)
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for messages")
		}
	}
}

// assertParityOutcome is the one set of expectations every mode must meet.
func assertParityOutcome(t *testing.T, turn parityTurn, got parityOutcome, sessionID, model, wantText string, wantUsage TokenUsage) {
	t.Helper()
	res := got.result
	if turn.wantErr != "" {
		if res.Status != "failed" {
			t.Fatalf("%s: status = %q, want failed (result %+v)", turn.taskID, res.Status, res)
		}
		if !strings.Contains(res.Error, turn.wantErr) {
			t.Fatalf("%s: error = %q, want it to contain %q", turn.taskID, res.Error, turn.wantErr)
		}
		return
	}
	if res.Status != "completed" || res.Error != "" {
		t.Fatalf("%s: result = %+v, want completed", turn.taskID, res)
	}
	if res.SessionID != sessionID {
		t.Fatalf("%s: session id = %q, want %q", turn.taskID, res.SessionID, sessionID)
	}
	if res.ResumeRejected {
		t.Fatalf("%s: resume reported rejected", turn.taskID)
	}
	if res.Output != wantText {
		t.Fatalf("%s: output = %q, want %q", turn.taskID, res.Output, wantText)
	}
	if got.text != wantText {
		t.Fatalf("%s: streamed text = %q, want %q", turn.taskID, got.text, wantText)
	}
	if !slices.Contains(got.statusSessionIDs, sessionID) {
		t.Fatalf("%s: no running status carried session %q (got %v)", turn.taskID, sessionID, got.statusSessionIDs)
	}
	if u := res.Usage[model]; u.InputTokens != wantUsage.InputTokens || u.OutputTokens != wantUsage.OutputTokens {
		t.Fatalf("%s: usage = %+v, want %+v", turn.taskID, res.Usage, wantUsage)
	}
}

func TestClaudeColdAndWarmTurnsShareLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	const sessionID = "claude-parity-1"
	const model = "claude-opus-5-5"

	// One process serves turns until stdin closes. Per-turn usage rides on the
	// assistant event and cumulative process totals on the result, the way
	// Claude Code reports them. The task id comes from the pooled overlay file
	// when there is one, otherwise from the inherited environment.
	script := `#!/bin/sh
printf '%s\n' "$$" >> "$SPAWN_LOG"
turn=0
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      turn=$((turn + 1))
      tid="$MULTICA_TASK_ID"
      if [ -n "$MULTICA_TASK_ENV_FILE" ]; then
        tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      fi
      if [ "$turn" = "1" ]; then
        printf '{"type":"system","subtype":"init","session_id":"` + sessionID + `"}\n'
      fi
      case "$line" in
        *'please fail'*)
          printf '{"type":"result","subtype":"error","session_id":"` + sessionID + `","result":"rate limit exceeded","is_error":true}\n'
          continue
          ;;
      esac
      printf '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg-%s"}}}\n' "$turn"
      printf '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}}\n'
      printf '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"%s"}}}\n' "$tid"
      printf '{"type":"stream_event","event":{"type":"message_stop"}}\n'
      printf '{"type":"assistant","message":{"id":"msg-%s","role":"assistant","model":"` + model + `","usage":{"input_tokens":50,"output_tokens":15},"content":[{"type":"text","text":"hello %s"}]}}\n' "$turn" "$tid"
      printf '{"type":"result","subtype":"success","session_id":"` + sessionID + `","result":"hello %s","is_error":false,"modelUsage":{"` + model + `":{"inputTokens":%d,"outputTokens":%d}}}\n' "$tid" $((turn * 50)) $((turn * 15))
      ;;
  esac
done
`

	for _, mode := range []struct {
		name       string
		pooled     bool
		wantSpawns int
	}{
		{name: "pool disabled", pooled: false, wantSpawns: len(parityConversation)},
		{name: "pool miss then hit", pooled: true, wantSpawns: 1},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			spawnLog := filepath.Join(dir, "spawns.log")
			fakeClaude := filepath.Join(dir, "claude")
			writeTestExecutable(t, fakeClaude, []byte(script))
			// A resumable session has a transcript on disk; a fresh process
			// resuming it measures the result's totals against that file.
			configDir := filepath.Join(dir, "claude-config")
			projectDir := filepath.Join(configDir, "projects", "parity")
			if err := os.MkdirAll(projectDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), nil, 0o600); err != nil {
				t.Fatal(err)
			}

			var pool *WarmSessionPool
			if mode.pooled {
				pool = NewWarmSessionPool(WarmSessionPoolConfig{
					IdleTTL: 30 * time.Second,
					MaxIdle: 4,
					Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
				})
				defer pool.Close()
			}

			for _, turn := range parityConversation {
				b, err := New("claude", Config{
					ExecutablePath: fakeClaude,
					Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
					TaskID:         turn.taskID,
					WarmPool:       pool,
					Env: map[string]string{
						"SPAWN_LOG":         spawnLog,
						"CLAUDE_CONFIG_DIR": configDir,
						"MULTICA_TOKEN":     "mat_" + turn.taskID,
						"MULTICA_TASK_ID":   turn.taskID,
						"MULTICA_AGENT_ID":  "agent-parity",
					},
				})
				if err != nil {
					t.Fatalf("New(claude): %v", err)
				}
				opts := ExecOptions{Cwd: dir, Model: model, Timeout: 15 * time.Second}
				if turn.resume {
					opts.ResumeSessionID = sessionID
				}
				sess, err := b.Execute(context.Background(), turn.prompt, opts)
				if err != nil {
					t.Fatalf("Execute(%s): %v", turn.taskID, err)
				}
				got := collectParityOutcome(t, sess)
				assertParityOutcome(t, turn, got, sessionID, model, "hello "+turn.taskID, TokenUsage{InputTokens: 50, OutputTokens: 15})
			}

			spawns, err := os.ReadFile(spawnLog)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(splitNonEmptyLines(string(spawns))); n != mode.wantSpawns {
				t.Fatalf("claude spawned %d times, want %d", n, mode.wantSpawns)
			}
			if pool.Len() != 0 {
				t.Fatalf("pool holds %d sessions after the failed turn, want 0", pool.Len())
			}
		})
	}
}

func TestCodexColdAndWarmTurnsShareLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	const threadID = "thr-parity-1"
	const model = "gpt-6.1-sol"

	// One app-server serves turns until stdin closes and reports cumulative
	// thread usage, so a reused process must report only the per-turn delta.
	script := `#!/bin/sh
printf '%s\n' "$$" >> "$SPAWN_LOG"
turn_num=0
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"userAgent":"fake-codex"}}\n' "$id"
      ;;
    *'"method":"thread/start"'*|*'"method":"thread/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread":{"id":"` + threadID + `"}}}\n' "$id"
      ;;
    *'"method":"turn/start"'*)
      turn_num=$((turn_num + 1))
      tid="$MULTICA_TASK_ID"
      if [ -n "$MULTICA_TASK_ENV_FILE" ]; then
        tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"turn":{"id":"turn-%s"}}}\n' "$id" "$turn_num"
      printf '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"` + threadID + `","turn":{"id":"turn-%s"}}}\n' "$turn_num"
      case "$line" in
        *'please fail'*)
          printf '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"` + threadID + `","turn":{"id":"turn-%s","status":"failed","error":{"message":"rate limit exceeded"}}}}\n' "$turn_num"
          continue
          ;;
      esac
      printf '{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"` + threadID + `","turnId":"turn-%s","tokenUsage":{"total":{"inputTokens":%d,"outputTokens":%d},"last":{"inputTokens":100,"outputTokens":10}}}}\n' "$turn_num" $((turn_num * 100)) $((turn_num * 10))
      printf '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"` + threadID + `","turnId":"turn-%s","item":{"type":"agentMessage","id":"msg-%s","phase":"final_answer","text":"hello %s"}}}\n' "$turn_num" "$turn_num" "$tid"
      printf '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"` + threadID + `","turn":{"id":"turn-%s","status":"completed"}}}\n' "$turn_num"
      ;;
  esac
done
`

	for _, mode := range []struct {
		name       string
		pooled     bool
		wantSpawns int
	}{
		{name: "pool disabled", pooled: false, wantSpawns: len(parityConversation)},
		{name: "pool miss then hit", pooled: true, wantSpawns: 1},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			spawnLog := filepath.Join(dir, "spawns.log")
			fakeCodex := filepath.Join(dir, "codex")
			writeTestExecutable(t, fakeCodex, []byte(script))
			codexHome := filepath.Join(dir, "codex-home")
			if err := os.MkdirAll(codexHome, 0o755); err != nil {
				t.Fatal(err)
			}

			var pool *WarmSessionPool
			if mode.pooled {
				pool = NewWarmSessionPool(WarmSessionPoolConfig{
					IdleTTL: 30 * time.Second,
					MaxIdle: 4,
					Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
				})
				defer pool.Close()
			}

			for _, turn := range parityConversation {
				b, err := New("codex", Config{
					ExecutablePath: fakeCodex,
					Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
					TaskID:         turn.taskID,
					WarmPool:       pool,
					Env: map[string]string{
						"SPAWN_LOG":        spawnLog,
						"CODEX_HOME":       codexHome,
						"MULTICA_TOKEN":    "mat_" + turn.taskID,
						"MULTICA_TASK_ID":  turn.taskID,
						"MULTICA_AGENT_ID": "agent-parity",
					},
				})
				if err != nil {
					t.Fatalf("New(codex): %v", err)
				}
				opts := ExecOptions{Cwd: dir, Model: model, Timeout: 15 * time.Second}
				if turn.resume {
					opts.ResumeSessionID = threadID
				}
				sess, err := b.Execute(context.Background(), turn.prompt, opts)
				if err != nil {
					t.Fatalf("Execute(%s): %v", turn.taskID, err)
				}
				got := collectParityOutcome(t, sess)
				assertParityOutcome(t, turn, got, threadID, model, "hello "+turn.taskID, TokenUsage{InputTokens: 100, OutputTokens: 10})
			}

			spawns, err := os.ReadFile(spawnLog)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(splitNonEmptyLines(string(spawns))); n != mode.wantSpawns {
				t.Fatalf("codex spawned %d times, want %d", n, mode.wantSpawns)
			}
			if pool.Len() != 0 {
				t.Fatalf("pool holds %d sessions after the failed turn, want 0", pool.Len())
			}
		})
	}
}

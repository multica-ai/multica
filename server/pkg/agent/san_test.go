package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fakeSanACP = `#!/bin/sh
[ "$#" = 1 ] && [ "$1" = acp ] || exit 2
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
  case "$method" in
    initialize)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%s,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true}}}}\n' "$id" "${SAN_TEST_PROTOCOL:-1}"
      ;;
    session/new|session/load)
      case "$line" in *'"cwd":"'*/work'"'*) ;; *) exit 3 ;; esac
      case "$line" in *'"url":"https://example.test/mcp"'*) ;; *) exit 4 ;; esac
      case "$line" in *'"type":"sse"'*) exit 5 ;; esac
      case "$method" in
        session/new)
          printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"new-session"}}\n' "$id"
          ;;
        session/load)
          case "$line" in *'"sessionId":"saved-session"'*) ;; *) exit 6 ;; esac
          if [ -n "$SAN_TEST_LOAD_ERROR" ]; then
            printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":%s}}\n' "$id" "$SAN_TEST_LOAD_ERROR"
            continue
          fi
          printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"saved-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"replayed history"}}}}'
          printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
          ;;
      esac
      ;;
    session/prompt)
      case "$line" in *'"text":"say pong"'*) ;; *) exit 7 ;; esac
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n' "$sid"
      if [ "$SAN_TEST_WAIT" = 1 ]; then continue; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"%s"}}\n' "$id" "${SAN_TEST_STOP:-end_turn}"
      ;;
  esac
done
`

func TestSanACP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	fakePath := filepath.Join(t.TempDir(), "san")
	writeTestExecutable(t, fakePath, []byte(fakeSanACP))
	for _, tc := range []struct {
		name, resume, status, id, output, wantError string
		env                                         map[string]string
		cancel, rejected                            bool
	}{
		{name: "new", status: "completed", id: "new-session", output: "pong"},
		{name: "load", resume: "saved-session", status: "completed", id: "saved-session", output: "pong"},
		{name: "missing session", resume: "saved-session", status: "failed", rejected: true, wantError: "session not found", env: map[string]string{"SAN_TEST_LOAD_ERROR": `"session not found"`}},
		{name: "load directory error", resume: "saved-session", status: "failed", wantError: "cwd mismatch", env: map[string]string{"SAN_TEST_LOAD_ERROR": `"cwd mismatch"`}},
		{name: "protocol mismatch", status: "failed", wantError: "expected ACP protocol version 1", env: map[string]string{"SAN_TEST_PROTOCOL": "2"}},
		{name: "truncated", status: "failed", id: "new-session", output: "pong", wantError: "max_tokens", env: map[string]string{"SAN_TEST_STOP": "max_tokens"}},
		{name: "cancelled", status: "aborted", id: "new-session", output: "pong", cancel: true, env: map[string]string{"SAN_TEST_WAIT": "1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend, err := New("san", Config{ExecutablePath: fakePath, Env: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			t.Chdir(cwd)
			if err := os.Mkdir("work", 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, deadline := context.WithTimeout(context.Background(), 10*time.Second)
			defer deadline()
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			session, err := backend.Execute(runCtx, "say pong", ExecOptions{
				Cwd: "work", ResumeSessionID: tc.resume,
				McpConfig: json.RawMessage(`{"mcpServers":{"http":{"url":"https://example.test/mcp","type":"http"},"sse":{"url":"https://example.test/sse","type":"sse"}}}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
		stream:
			for {
				select {
				case msg, ok := <-session.Messages:
					if !ok {
						break stream
					}
					if msg.Type == MessageText {
						text.WriteString(msg.Content)
						if tc.cancel {
							cancel()
						}
					}
				case <-ctx.Done():
					t.Fatal("San did not close its stream")
				}
			}
			result := <-session.Result
			if result.Status != tc.status || result.SessionID != tc.id || result.Output != tc.output || result.ResumeRejected != tc.rejected ||
				!strings.Contains(result.Error, tc.wantError) || text.String() != tc.output {
				t.Fatalf("unexpected result %+v; streamed text %q", result, text.String())
			}
		})
	}
}

func TestSanPermissionDeclinesUnattendedApproval(t *testing.T) {
	id, granted, ok := selectSanPermission(json.RawMessage(`{"options":[{"optionId":"allow","kind":"allow_once"},{"optionId":"reject","kind":"reject_once"}]}`))
	if id != "reject" || granted || !ok {
		t.Fatalf("permission decision = %q, %t, %t", id, granted, ok)
	}
	if _, _, ok := selectSanPermission(json.RawMessage(`{"options":[{"optionId":"allow","kind":"allow_once"}]}`)); ok {
		t.Fatal("approved a request that requires human consent")
	}
}

package agent

import (
	"runtime"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestCodexReconnectBeforeModelProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	const setup = `read line
echo '{"id":1,"result":{}}'
read line
read line
echo '{"id":2,"result":{"thread":{"id":"thr"}}}'
read line
echo '{"id":3,"result":{}}'
event() { printf '{"method":"%s","params":{"threadId":"thr","turnId":"turn",%s}}\n' "$1" "$2"; }
started() { event turn/started '"turn":{"id":"turn"}'; }
user_input() {
  event item/started '"item":{"type":"userMessage","id":"input"}'
  event item/completed '"item":{"type":"userMessage","id":"input"}'
}
reasoning() { event item/started '"item":{"type":"reasoning","id":"r"}'; }
user_shell() {
  event item/started '"item":{"id":"cmd","type":"commandExecution","source":"userShell"}'
  event item/commandExecution/outputDelta '"itemId":"cmd","delta":"local output"'
}
reconnect() { event error '"willRetry":true,"error":{"message":"Reconnecting... 1/5"}'; }
offline() { event error '"willRetry":true,"error":{"message":"Reconnecting... waiting for network"}'; }
loop() { for i in 1 2 3 4 5 6 7 8; do offline; sleep 0.04; done; }
finish() {
  event item/completed '"item":{"type":"agentMessage","id":"answer","text":"ok"}'
  event turn/completed '"turn":{"id":"turn","status":"completed"}'
}
`
	for _, tc := range []struct {
		name, events string
		fails        bool
	}{
		{"only reconnects", "started; reconnect; loop; finish", true},
		{"no start notification", "reconnect; loop; finish", true},
		{"input echo is not progress", "started; user_input; reconnect; loop; finish", true},
		{"client shell command is not model progress", "started; user_shell; reconnect; loop; finish", true},
		{"unrelated retries do not arm reconnect deadline", `started; user_input; event error '"willRetry":true,"error":{"message":"retrying a temporary service error"}'; sleep 0.32; finish`, false},
		{"recovered", "started; reconnect; offline; reasoning; sleep 0.32; finish", false},
		{"disconnect after reasoning", "started; reasoning; reconnect; loop; finish", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFakeCodexAppServer(t, setup+tc.events+"\n")
			result := executeFakeCodex(t, path, ExecOptions{
				Timeout: 5 * time.Second, CodexReconnectTimeout: 160 * time.Millisecond,
				FirstTurnNoProgressTimeout: 80 * time.Millisecond, SemanticInactivityTimeout: 3 * time.Second,
				TurnInterruptTimeout: 100 * time.Millisecond,
			})
			if tc.fails {
				if result.Status != "failed" || result.Error != taskfailure.CodexReconnectFailure ||
					taskfailure.Classify(result.Error) != taskfailure.ReasonAgentProviderNetwork {
					t.Fatalf("expected model connection failure, got %+v", result)
				}
			} else if result.Status != "completed" || result.Output != "ok" {
				t.Fatalf("expected completed reply, got %+v", result)
			}
		})
	}
}

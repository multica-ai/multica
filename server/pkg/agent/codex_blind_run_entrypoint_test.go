package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexExecuteBlindRunDiagnostic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	for _, tc := range []struct {
		name    string
		outputs []string
		warn    bool
	}{
		{"empty", []string{"", "", ""}, true},
		{"mixed", []string{"", "ok", ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `read line
 echo '{"jsonrpc":"2.0","id":1,"result":{}}'
 read line
 read line
 echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-blind"}}}'
 read line
 echo '{"jsonrpc":"2.0","id":3,"result":{}}'
 echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-blind","turn":{"id":"turn-blind"}}}'
`
			for i, output := range tc.outputs {
				event, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{"threadId": "thr-blind", "turnId": "turn-blind", "item": map[string]any{"type": "commandExecution", "id": fmt.Sprint(i), "aggregatedOutput": output}}})
				if err != nil {
					t.Fatal(err)
				}
				body += "echo '" + string(event) + "'\n"
			}
			body += `echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-blind","item":{"type":"agentMessage","id":"msg-1","text":"Done"}}}'
 echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-blind","turn":{"id":"turn-blind","status":"completed"}}}'
`
			// The no-usage fixture must not scan the developer's real sessions
			// through the existing usage fallback before returning its result.
			fixtureHome := t.TempDir()
			if err := os.Mkdir(filepath.Join(fixtureHome, "sessions"), 0o755); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			result, _ := executeFakeCodexCollectingMessagesWithConfig(t, writeFakeCodexAppServer(t, body), Config{Logger: slog.New(slog.NewTextHandler(&logs, nil)), Env: map[string]string{"CODEX_HOME": fixtureHome}}, ExecOptions{Timeout: 5 * time.Second}, 10*time.Second)
			if result.Status != "completed" || result.Output != "Done" {
				t.Fatalf("result = %+v", result)
			}
			if got := strings.Contains(logs.String(), "codex blind run detected"); got != tc.warn {
				t.Fatalf("diagnostic present=%v want=%v; logs=%s", got, tc.warn, logs.String())
			}
		})
	}
}

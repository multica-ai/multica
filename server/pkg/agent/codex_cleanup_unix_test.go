//go:build unix

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCodexThreadStartTimeoutReapsDetachedStdioDescendant(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "descendant.pid")
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`sleep 30 >/dev/null 2>&1 & echo $! > "`+pidFile+`"`+"\n"+
		`sleep 1.2`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-late"}}}'`+"\n"+
		`read line`+"\n")

	var logs strings.Builder
	backend, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected thread/start failure, got %+v", result)
	}
	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, pid)
	failure := findCodexLifecyclePhase(t, parseJSONLogEntries(t, logs.String()), "thread_start_failure")
	if failure["reaped"] != true || failure["cleanup_confirmed"] != true {
		t.Fatalf("process-tree cleanup not confirmed: %v", failure)
	}
}

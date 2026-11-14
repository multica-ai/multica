package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexOverloadRetryRejectedResumeCannotStartNewThread(t *testing.T) {
	opts := ExecOptions{Cwd: "/work", ResumeSessionID: "thr_prior", ResumeExpected: true, CodexRetryMustResume: true}
	c, fs, _ := newTestCodexClient(t)
	wait := drainRPCScript(t, c, fs, []rpcResponse{{method: "thread/resume", errMsg: "unknown thread", errCode: -32602}})
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	threadID, resumed, err := c.startOrResumeThread(ctx, opts, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "thread/resume") || threadID != "" || resumed {
		t.Fatalf("required resume rejection must fail without new thread: id=%q resumed=%v err=%v", threadID, resumed, err)
	}
	if got := len(fs.Lines()); got != 1 {
		t.Fatalf("RPC count=%d, want only thread/resume; thread/start would lose retry context", got)
	}
}

func TestCodexOverloadRetryMissingSessionCannotStartThread(t *testing.T) {
	c, fs, _ := newTestCodexClient(t)
	id, resumed, err := c.startOrResumeThread(context.Background(), ExecOptions{CodexRetryMustResume: true}, slog.Default())
	if err == nil || id != "" || resumed || len(fs.Lines()) != 0 {
		t.Fatalf("missing required resume must fail before RPC: id=%q resumed=%v calls=%d err=%v", id, resumed, len(fs.Lines()), err)
	}
}

func TestCodexOverloadRetryEmptyResumeCannotStartThread(t *testing.T) {
	c, fs, _ := newTestCodexClient(t)
	wait := drainRPCScript(t, c, fs, []rpcResponse{{method: "thread/resume", result: json.RawMessage(`{"thread":{}}`)}})
	defer wait()
	id, resumed, err := c.startOrResumeThread(context.Background(), ExecOptions{ResumeSessionID: "thr_prior", CodexRetryMustResume: true}, slog.Default())
	if err == nil || id != "" || resumed || len(fs.Lines()) != 1 {
		t.Fatalf("empty required resume must fail before thread/start: id=%q resumed=%v calls=%d err=%v", id, resumed, len(fs.Lines()), err)
	}
}

func TestCodexOverloadRetryDoesNotClearResumeAfterCatalogFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() { codexGracefulShutdownTimeoutNanos.Store(0) })
	fakePath := writeFakeCodexAppServer(t, ""+
		`DIR="$(dirname "$0")"`+"\n"+
		`ATTEMPT=$(cat "$DIR/attempts" 2>/dev/null || echo 0)`+"\n"+
		`ATTEMPT=$((ATTEMPT+1))`+"\n"+
		`echo "$ATTEMPT" > "$DIR/attempts"`+"\n"+
		`read line; echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line; echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-prior"}}}'`+"\n"+
		`read line; echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-prior","turn":{"id":"turn-prior"}}}'`+"\n"+
		`echo 'ERROR codex_models_manager::manager: failed to refresh available models: timeout waiting for child process to exit' >&2`+"\n"+
		`sleep 2`+"\n")
	result, _ := executeFakeCodexCollectingMessages(t, fakePath, ExecOptions{
		ResumeSessionID: "thr-prior", CodexRetryMustResume: true,
		Timeout: 20 * time.Second, SemanticInactivityTimeout: 100 * time.Millisecond,
	}, 20*time.Second)
	if result.Status == "completed" {
		t.Fatalf("required resume unexpectedly completed on another process: %+v", result)
	}
	attempts, err := os.ReadFile(filepath.Join(filepath.Dir(fakePath), "attempts"))
	if err != nil || strings.TrimSpace(string(attempts)) != "1" {
		t.Fatalf("Codex launches=%q err=%v, want one", attempts, err)
	}
}

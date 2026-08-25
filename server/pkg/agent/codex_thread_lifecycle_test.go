package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCodexThreadLifecycleRPCUsesOfficialShapeAndRuntimeIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	requestLog := filepath.Join(t.TempDir(), "requests.jsonl")
	identityLog := filepath.Join(t.TempDir(), "identity.txt")
	fakePath := writeFakeCodexAppServer(t, ""+
		`printf '%s\n' "$CODEX_HOME" > "$LIFECYCLE_IDENTITY_LOG"`+"\n"+
		`printf '%s\n' "$@" >> "$LIFECYCLE_IDENTITY_LOG"`+"\n"+
		`IFS= read -r line; printf '%s\n' "$line" >> "$LIFECYCLE_REQUEST_LOG"`+"\n"+
		`printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`IFS= read -r line; printf '%s\n' "$line" >> "$LIFECYCLE_REQUEST_LOG"`+"\n"+
		`IFS= read -r line; printf '%s\n' "$line" >> "$LIFECYCLE_REQUEST_LOG"`+"\n"+
		`printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{}}'`+"\n")

	backendAny, err := New("codex", Config{
		ExecutablePath: fakePath,
		LaunchPrefix:   []string{"profile-prefix"},
		Env: map[string]string{
			"CODEX_HOME":             home,
			"LIFECYCLE_REQUEST_LOG":  requestLog,
			"LIFECYCLE_IDENTITY_LOG": identityLog,
		},
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, ok := backendAny.(CodexThreadLifecycle)
	if !ok {
		t.Fatal("codex backend does not implement CodexThreadLifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.ArchiveThread(ctx, "thr-owned", ExecOptions{Cwd: t.TempDir()}); err != nil {
		t.Fatalf("ArchiveThread: %v", err)
	}
	if err := lifecycle.UnarchiveThread(ctx, "thr-owned", ExecOptions{Cwd: t.TempDir()}); err != nil {
		t.Fatalf("UnarchiveThread: %v", err)
	}

	raw, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 6 {
		t.Fatalf("request lines = %d, want archive and unarchive handshakes: %q", len(lines), raw)
	}
	var initialize, initialized, archive struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &initialized); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &archive); err != nil {
		t.Fatal(err)
	}
	if initialize.Method != "initialize" || initialized.Method != "initialized" || archive.Method != "thread/archive" {
		t.Fatalf("method sequence = %q, %q, %q", initialize.Method, initialized.Method, archive.Method)
	}
	if got := archive.Params["threadId"]; got != "thr-owned" {
		t.Fatalf("thread/archive threadId = %v, want thr-owned", got)
	}
	var unarchive struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(lines[5]), &unarchive); err != nil {
		t.Fatal(err)
	}
	if unarchive.Method != "thread/unarchive" || unarchive.Params["threadId"] != "thr-owned" {
		t.Fatalf("thread/unarchive request = %+v", unarchive)
	}

	identity, err := os.ReadFile(identityLog)
	if err != nil {
		t.Fatal(err)
	}
	identityLines := strings.Split(strings.TrimSpace(string(identity)), "\n")
	if len(identityLines) < 2 || identityLines[0] != home || identityLines[1] != "profile-prefix" {
		t.Fatalf("lifecycle runtime identity = %q, want CODEX_HOME then launch prefix", identity)
	}
}

func TestCodexThreadIDsDeduplicatesAndCapsTargets(t *testing.T) {
	b := &codexBackend{}
	b.recordThreadID("thr-a")
	b.recordThreadID("thr-a")
	for i := 0; i < maxCodexArchiveTargets+3; i++ {
		b.recordThreadID(string(rune('b' + i)))
	}
	ids := b.ThreadIDs()
	if len(ids) != maxCodexArchiveTargets {
		t.Fatalf("ThreadIDs len = %d, want cap %d: %v", len(ids), maxCodexArchiveTargets, ids)
	}
	if ids[0] != "thr-a" {
		t.Fatalf("first thread id = %q, want deduplicated thr-a", ids[0])
	}
	ids[0] = "mutated"
	if b.ThreadIDs()[0] != "thr-a" {
		t.Fatal("ThreadIDs exposed backend storage")
	}
}

func TestCodexThreadLifecycleRPCBoundsAndReapsProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script process-group fixture is POSIX-only")
	}
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	fakePath := writeFakeCodexAppServer(t, ""+
		`sleep 30 & child=$!`+"\n"+
		`printf '%s\n' "$child" > "$LIFECYCLE_CHILD_PID"`+"\n"+
		`IFS= read -r line`+"\n"+
		`wait "$child"`+"\n")
	backendAny, err := New("codex", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"LIFECYCLE_CHILD_PID": childPIDFile},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := backendAny.(CodexThreadLifecycle)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = lifecycle.ArchiveThread(ctx, "thr-timeout", ExecOptions{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("ArchiveThread error = %v, want bounded timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("lifecycle timeout took %s, want bounded cleanup", elapsed)
	}

	rawPID, readErr := os.ReadFile(childPIDFile)
	if readErr != nil {
		t.Fatalf("fake app-server did not spawn child: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if convErr != nil {
		t.Fatal(convErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		process, _ := os.FindProcess(pid)
		alive := process != nil && process.Signal(syscall.Signal(0)) == nil
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lifecycle descendant pid %d survived process-group cleanup", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCodexThreadLifecycleRPCDoesNotExposeProviderError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	const secret = "SECRET_PROVIDER_DIAGNOSTIC"
	fakePath := writeFakeCodexAppServer(t, ""+
		`IFS= read -r line`+"\n"+
		`printf '%s\n' '{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"`+secret+`"}}'`+"\n")
	backendAny, err := New("codex", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	err = backendAny.(CodexThreadLifecycle).ArchiveThread(context.Background(), "thr-secret", ExecOptions{})
	if err == nil {
		t.Fatal("ArchiveThread unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("lifecycle error exposed provider text: %v", err)
	}
}

func TestCodexThreadLifecycleClientRejectsServerToolRequests(t *testing.T) {
	stdin := &fakeStdin{}
	client := &codexClient{
		cfg:                  Config{Logger: slog.Default()},
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		processDone:          make(chan struct{}),
		rejectServerRequests: true,
	}
	client.handleLine(`{"jsonrpc":"2.0","id":41,"method":"item/commandExecution/requestApproval","params":{}}`)
	lines := stdin.Lines()
	if len(lines) != 1 {
		t.Fatalf("lifecycle approval responses = %d, want one explicit rejection", len(lines))
	}
	var response struct {
		ID    int `json:"id"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
		Result any `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 41 || response.Error.Code != -32601 || response.Result != nil {
		t.Fatalf("lifecycle server request response = %s, want JSON-RPC rejection", lines[0])
	}
}

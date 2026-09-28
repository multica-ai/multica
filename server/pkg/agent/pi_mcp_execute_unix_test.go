//go:build unix

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPiManagedMCPExecution(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			original := piMCPFixture(t)
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture")
			argsPath := filepath.Join(dir, "args")
			marker := ""
			if ready {
				marker = `printf '%s\n' '{"type":"multica_managed_mcp_ready"}'`
			}
			script := fmt.Sprintf(`#!/bin/sh
printf '%%s' "$PI_CODING_AGENT_DIR" > %q
printf '%%s\n' "$@" > %q
cat > /dev/null
%s
printf '%%s\n' '{"type":"agent_start"}'
printf '%%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"private-"}}'
printf '%%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"token"}}'
printf '%%s\n' 'connection rejected: private-token' >&2
`, capture, argsPath, marker)
			path := filepath.Join(dir, "fake-pi")
			writeTestExecutable(t, path, []byte(script))
			var logs bytes.Buffer
			backend, _ := New("pi", Config{ExecutablePath: path, Env: map[string]string{"PI_CODING_AGENT_DIR": original}, Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
			session, err := backend.Execute(context.Background(), "test", ExecOptions{Cwd: dir, ResumeSessionID: filepath.Join(dir, "session"), Timeout: 5 * time.Second, McpConfig: json.RawMessage(`{"mcpServers":{"a":{"url":"http://localhost/mcp","headers":{"Authorization":"private-token"}}}}`)})
			if err != nil {
				t.Fatal(err)
			}
			var messages strings.Builder
			done := make(chan struct{})
			go func() {
				defer close(done)
				for m := range session.Messages {
					messages.WriteString(m.Content)
				}
			}()
			result := <-session.Result
			<-done
			envDir, _ := os.ReadFile(capture)
			args, _ := os.ReadFile(argsPath)
			if string(envDir) == original || !strings.Contains(string(args), "--no-extensions") {
				t.Fatal("managed environment not isolated")
			}
			if _, err := os.Stat(string(envDir)); !os.IsNotExist(err) {
				t.Fatal("run directory not cleaned before result")
			}
			if ready && result.Status != "completed" {
				t.Fatalf("%+v", result)
			}
			if !ready && (result.Status != "failed" || !strings.Contains(result.Error, "managed MCP")) {
				t.Fatalf("missing adapter silently succeeded: %+v", result)
			}
			if strings.Contains(logs.String()+result.Output+messages.String(), "private-token") {
				t.Fatal("MCP secret leaked")
			}
		})
	}
}

func TestPiManagedMCPRejectsIsolationOverrides(t *testing.T) {
	for _, args := range [][]string{{"-e", "evil.ts"}, {"--extension=evil.ts"}, {"--mcp-config", "global.json"}, {"-ne"}} {
		if err := validatePiMCPArgs(args); err == nil {
			t.Fatalf("accepted override %v", args)
		}
	}
}

func TestPiManagedMCPCancellationCleansAndNativeRunNeedsNoPlugin(t *testing.T) {
	dir := t.TempDir()
	original := piMCPFixture(t)
	capture := filepath.Join(dir, "capture")
	path := filepath.Join(dir, "fake-pi")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s' "$PI_CODING_AGENT_DIR" > %q
cat > /dev/null
printf '%%s\n' '{"type":"multica_managed_mcp_ready"}' '{"type":"agent_start"}'
sleep 30
`, capture)
	writeTestExecutable(t, path, []byte(script))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	backend, _ := New("pi", Config{ExecutablePath: path, Env: map[string]string{"PI_CODING_AGENT_DIR": original}, Logger: slog.Default()})
	session, err := backend.Execute(ctx, "test", ExecOptions{ResumeSessionID: filepath.Join(dir, "session"), McpConfig: json.RawMessage(`{}`), Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for m := range session.Messages {
		if m.Status == "running" {
			cancel()
			break
		}
	}
	go func(messages <-chan Message) {
		for range messages {
		}
	}(session.Messages)
	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Fatalf("%+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop process")
	}
	private, _ := os.ReadFile(capture)
	if _, err := os.Stat(string(private)); !os.IsNotExist(err) {
		t.Fatal("cancel leaked private environment")
	}
	// A missing adapter is irrelevant for native/null configuration.
	writeTestExecutable(t, path, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"agent_start\"}'\n"))
	backend, _ = New("pi", Config{ExecutablePath: path, Env: map[string]string{"PI_CODING_AGENT_DIR": t.TempDir()}, Logger: slog.Default()})
	session, err = backend.Execute(t.Context(), "native", ExecOptions{ResumeSessionID: filepath.Join(dir, "native"), McpConfig: json.RawMessage(`null`), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func(messages <-chan Message) {
		for range messages {
		}
	}(session.Messages)
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("native Pi blocked: %+v", result)
	}
}

func TestPiManagedMCPStartFailureRemovesPrivateDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	original := piMCPFixture(t)
	path := filepath.Join(dir, "fake-pi")
	writeTestExecutable(t, path, []byte("#!/does-not-exist\n"))
	backend, _ := New("pi", Config{ExecutablePath: path, Env: map[string]string{"PI_CODING_AGENT_DIR": original}, Logger: slog.Default()})
	_, err := backend.Execute(t.Context(), "test", ExecOptions{ResumeSessionID: filepath.Join(dir, "session"), McpConfig: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("expected failed start")
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "multica-pi-mcp-*"))
	if len(entries) != 0 {
		t.Fatalf("private state leaked: %v", entries)
	}
}

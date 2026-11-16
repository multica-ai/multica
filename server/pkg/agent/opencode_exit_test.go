package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Re-execute the test binary so Windows exercises a native process exit,
// without a shell, an installed OpenCode, or a provider account.
func TestOpencodeExitHelperProcess(t *testing.T) {
	mode := os.Getenv("MULTICA_TEST_OPENCODE_EXIT")
	if mode == "" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(2)
	}
	if mode != "empty" {
		fmt.Println(`{"type":"step_start","sessionID":"ses_exit","part":{"type":"step-start"}}`)
		if mode != "void" && mode != "usage" {
			fmt.Println(`{"type":"text","sessionID":"ses_exit","part":{"type":"text","text":"All done."}}`)
		}
		if mode != "unfinished" {
			reason := "stop"
			if mode == "continuation" {
				reason = "tool-calls"
			}
			input, output := 8, 2
			if mode == "void" {
				input, output = 0, 0
			}
			fmt.Printf("{\"type\":\"step_finish\",\"sessionID\":\"ses_exit\",\"part\":{\"type\":\"step-finish\",\"reason\":%q,\"tokens\":{\"input\":%d,\"output\":%d}}}\n", reason, input, output)
		}
		if mode == "error" {
			fmt.Println(`{"type":"error","sessionID":"ses_exit","error":{"name":"UnknownError","data":{"message":"synthetic provider failure"}}}`)
		}
	}
	exitCode := 134
	if runtime.GOOS == "windows" {
		// ExitProcess converts this signed int to STATUS_STACK_BUFFER_OVERRUN.
		exitCode = -1073740791 // 0xc0000409
	}
	os.Exit(exitCode)
}

func TestOpencodeBackendCrashOnExit(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mode, version, status, errorText string
		builtin                                bool
	}{
		{"v1 terminal", "complete", "1.17.16", "completed", "", true},
		{"custom terminal", "complete", "", "completed", "", false},
		{"v1 usage only", "usage", "1.17.16", "completed", "", true},
		{"v1 no events", "empty", "1.17.16", "failed", "opencode exited with error", true},
		{"v1 unfinished step", "unfinished", "1.17.16", "failed", "step still open", true},
		{"v1 tool continuation", "continuation", "1.17.16", "failed", "required a continuation", true},
		{"v1 void step", "void", "1.17.16", "failed", "empty step", true},
		{"v1 explicit error after terminal", "error", "1.17.16", "failed", "synthetic provider failure", true},
		{"v2 closed step", "complete", "2.0.10", "failed", "opencode exited with error", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			backend, err := New("opencode", Config{
				ExecutablePath: executable,
				LaunchPrefix:   []string{"-test.run=^TestOpencodeExitHelperProcess$", "--"},
				CLIVersion:     tc.version,
				BuiltinRuntime: tc.builtin,
				Logger:         quietAntigravityLogger(),
				Env: map[string]string{
					"MULTICA_TEST_OPENCODE_EXIT": tc.mode,
					opencodeTransportEnv:         "run",
					"HOME":                       home,
					"USERPROFILE":                home,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "synthetic prompt", ExecOptions{Cwd: home, Model: "synthetic/model"})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result, ok := <-session.Result:
				if !ok {
					t.Fatal("result channel closed without a result")
				}
				if result.Status != tc.status {
					t.Fatalf("status=%q, want %q; error=%q", result.Status, tc.status, result.Error)
				}
				if tc.status == "completed" {
					wantOutput := "All done."
					if tc.mode == "usage" {
						wantOutput = ""
					}
					usage := result.Usage["synthetic/model"]
					if result.Error != "" || result.Output != wantOutput || result.SessionID != "ses_exit" || usage.InputTokens != 8 || usage.OutputTokens != 2 {
						t.Fatalf("completed result lost output/session/usage: %+v", result)
					}
				} else if !strings.Contains(result.Error, tc.errorText) {
					t.Fatalf("error=%q, want %q", result.Error, tc.errorText)
				}
				if tc.mode == "empty" || tc.version == "2.0.10" {
					wantExit := "exit status 134"
					if runtime.GOOS == "windows" {
						wantExit = "exit status 0xc0000409"
					}
					if !strings.Contains(result.Error, wantExit) {
						t.Fatalf("native exit status missing from error: %q, want %q", result.Error, wantExit)
					}
					t.Logf("observed native %s", wantExit)
				}
			case <-ctx.Done():
				t.Fatal("result did not arrive within the fixture deadline")
			}
		})
	}
}

//go:build windows

package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	openclawShimHelperEnv      = "MULTICA_OPENCLAW_SHIM_HELPER"
	openclawShimHelperArgvFile = "MULTICA_OPENCLAW_SHIM_ARGV_FILE"
	openclawShimHelperMsgFile  = "MULTICA_OPENCLAW_SHIM_MSG_FILE"
)

// TestOpenclawWindowsShimHelperProcess is not a test. Re-executed by the
// fake openclaw.cmd below, it stands in for `node openclaw.mjs` as the real
// native child. Inert unless the shim env var is set.
func TestOpenclawWindowsShimHelperProcess(t *testing.T) {
	if os.Getenv(openclawShimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}
	// Direct invocation by the shim: os.Args is the shim's own argv forwarding
	// (openclaw agent --json ... --message-file ...). Also handle `--` sentinel
	// for compatibility with any future wrapper that inserts it.
	forwarded := os.Args[1:]
	for i, a := range os.Args {
		if a == "--" {
			forwarded = os.Args[i+1:]
			break
		}
	}
	if len(forwarded) == 0 {
		fmt.Fprintf(os.Stderr, "helper: no forwarded args; os.Args=%q\n", os.Args)
		os.Exit(1)
	}
	// --version probe
	if len(forwarded) == 1 && forwarded[0] == "--version" {
		fmt.Println("openclaw 2026.7.1")
		os.Exit(0)
	}
	if len(forwarded) >= 2 && forwarded[0] == "agent" && forwarded[1] == "--help" {
		fmt.Println("Usage: openclaw agent")
		fmt.Println("  --message <text>")
		fmt.Println("  --message-file <path>")
		os.Exit(0)
	}
	// Actual agent invocation: record argv and message-file content.
	if argvPath := os.Getenv(openclawShimHelperArgvFile); argvPath != "" {
		if err := os.WriteFile(argvPath, []byte(strings.Join(forwarded, "\n")), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
			os.Exit(1)
		}
	}
	var mf string
	for i, a := range forwarded {
		if a == "--message-file" && i+1 < len(forwarded) {
			mf = forwarded[i+1]
			break
		}
	}
	if mf != "" {
		data, err := os.ReadFile(mf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper: read message-file %q: %v\n", mf, err)
			os.Exit(1)
		}
		if dst := os.Getenv(openclawShimHelperMsgFile); dst != "" {
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "helper: write msg: %v\n", err)
				os.Exit(1)
			}
		}
		// Ensure --message is not also present (capability-aware transport uses file only).
		for _, a := range forwarded {
			if a == "--message" {
				fmt.Fprintf(os.Stderr, "helper: unexpected --message alongside --message-file\n")
				os.Exit(1)
			}
		}
	} else {
		// For this narrow fix the shim path always uses --message-file when
		// the capability is advertised; fail if missing.
		fmt.Fprintf(os.Stderr, "helper: missing --message-file in argv %q\n", strings.Join(forwarded, " "))
		os.Exit(1)
	}
	fmt.Println(`{"payloads":[{"text":"ok"}],"meta":{}}`)
	os.Exit(0)
}

// TestOpenclawExecutePromptViaMessageFileOnWindowsShim is the Windows half
// of the openclaw --message-file regression. It crosses the boundary the bug
// lived on:
//
//	Go os/exec -> cmd.exe -> openclaw.cmd -> native child (test binary)
//
// The .cmd shim is the 8191-char cmd.exe limit site and the CreateProcess
// 32767 limit site. A quote-heavy prompt inlined on argv would overflow the
// shim before the OS reports "command line too long". Delivering it via
// --message-file keeps argv short, preserves UTF-8 byte-for-byte (including
// SystemPrompt + "\n\n" + prompt), and cleans up the temp file before Result
// is observable.
//
// Also asserts the reported 6193-byte production size routes through the
// file on a supported .cmd shim.
//
// Runs only on windows-latest via ci.yml windows-execenv, which scopes
// -run to windows-tagged tests. -v makes a silent skip visible.
func TestOpenclawExecutePromptViaMessageFileOnWindowsShim(t *testing.T) {
	// No Parallel: sets process-wide env via t.Setenv.
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}

	runOne := func(t *testing.T, prompt, systemPrompt string) {
		t.Helper()
		dir := t.TempDir()
		argvPath := filepath.Join(dir, "argv.txt")
		msgContentPath := filepath.Join(dir, "msg.txt")

		cmdPath := filepath.Join(dir, "openclaw.cmd")
		// %* is the shim's own argv forwarding. Avoid ^/$ regex anchors to keep cmd.exe from stripping them.
		body := fmt.Sprintf("@echo off\r\n\"%s\" -test.run=TestOpenclawWindowsShimHelperProcess -- %%*\r\n", self)
		if err := os.WriteFile(cmdPath, []byte(body), 0o644); err != nil {
			t.Fatalf("write shim: %v", err)
		}

		t.Setenv(openclawShimHelperEnv, "1")
		t.Setenv(openclawShimHelperArgvFile, argvPath)
		t.Setenv(openclawShimHelperMsgFile, msgContentPath)
		helperEnv := map[string]string{
			openclawShimHelperEnv:      "1",
			openclawShimHelperArgvFile: argvPath,
			openclawShimHelperMsgFile:  msgContentPath,
		}

		backend, err := New("openclaw", Config{ExecutablePath: cmdPath, Logger: slog.Default(), Env: helperEnv})
		if err != nil {
			t.Fatalf("New(openclaw): %v", err)
		}
		session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 60 * time.Second, SystemPrompt: systemPrompt})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		go func() {
			for range session.Messages {
			}
		}()
		result := <-session.Result
		if result.Status != "completed" {
			t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
		}

		argvRaw, err := os.ReadFile(argvPath)
		if err != nil {
			t.Fatalf("native child never recorded argv (did shim reach helper?): %v; result=%+v", err, result)
		}
		gotArgv := string(argvRaw)
		if !strings.Contains(gotArgv, "--message-file") {
			t.Errorf("expected --message-file in argv; got %q", gotArgv)
		}
		if strings.Contains(gotArgv, "--message\n") {
			t.Errorf("prompt must not be inlined via --message on argv; argv=%q", gotArgv)
		}
		// Prompt fragments must not leak onto argv.
		for _, needle := range []string{"quoted", "gateway dispatch", "SYSTEM"} {
			if strings.Contains(gotArgv, needle) {
				t.Errorf("prompt fragment %q leaked into argv %q", needle, gotArgv)
			}
		}
		for _, want := range []string{"agent", "--json", "--session-id", "--message-file"} {
			if !strings.Contains(gotArgv, want) {
				t.Errorf("expected %q to reach native child; argv=%q", want, gotArgv)
			}
		}
		if len(gotArgv) > 8000 {
			t.Errorf("argv is %d chars, should be short via --message-file", len(gotArgv))
		}

		msgRaw, err := os.ReadFile(msgContentPath)
		if err != nil {
			t.Fatalf("helper never recorded message-file content: %v", err)
		}
		expected := prompt
		if systemPrompt != "" {
			expected = systemPrompt + "\n\n" + prompt
		}
		if string(msgRaw) != expected {
			t.Errorf("file content mismatch: got %d bytes want %d", len(msgRaw), len(expected))
		}
		if len(msgRaw) >= 3 && msgRaw[0] == 0xEF && msgRaw[1] == 0xBB && msgRaw[2] == 0xBF {
			t.Error("message file must not contain UTF-8 BOM")
		}

		// Cleanup: the temp message file must be removed before Result is
		// observable (cleanup happens before resCh <- Result).
		lines := strings.Split(strings.TrimSuffix(gotArgv, "\n"), "\n")
		var mfPath string
		for i, a := range lines {
			if a == "--message-file" && i+1 < len(lines) {
				mfPath = strings.TrimSpace(lines[i+1])
				break
			}
		}
		if mfPath == "" {
			t.Fatalf("could not extract --message-file path from argv %q", gotArgv)
		}
		if _, err := os.Stat(mfPath); err == nil {
			t.Errorf("message file %q still exists after session; cleanup missing", mfPath)
		}
		if _, err := os.Stat(filepath.Dir(mfPath)); err == nil {
			t.Errorf("message temp dir %q still exists after session; cleanup missing", filepath.Dir(mfPath))
		}
	}

	t.Run("quote-heavy payload exceeds 8191 serialized chars", func(t *testing.T) {
		// ASCII quote-heavy payload that definitely exceeds cmd.exe 8191 when
		// inlined: 900 * 10 = 9000 plus JSON/session overhead.
		// Using quotes and spaces exercises cmd.exe tokenization.
		prompt := strings.Repeat(`"quoted" `, 900) + "\n— gateway dispatch test —\n" + strings.Repeat("A", 1000)
		systemPrompt := "SYSTEM: you are a test harness"
		runOne(t, prompt, systemPrompt)
	})

	t.Run("reported 6193-byte prompt also routes via file", func(t *testing.T) {
		// Exactly 6193 bytes, the production report size. Must also use
		// --message-file on a supported .cmd shim, not inline argv.
		prompt := strings.Repeat("x", 6193)
		if len(prompt) != 6193 {
			t.Fatalf("6193-byte fixture is %d", len(prompt))
		}
		runOne(t, prompt, "")
	})
}

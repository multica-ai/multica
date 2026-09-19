package execenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// snippetLines returns the indented command block of a rendered reply
// cookbook — the lines an agent actually pastes into one shell call.
func snippetLines(t *testing.T, rendered string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimSpace(line))
		}
	}
	if len(out) == 0 {
		t.Fatalf("reply instructions contain no command block\n---\n%s", rendered)
	}
	return out
}

// stubMultica puts a fake `multica` on PATH that exits with the given code, so
// the snippet's failure handling can be exercised without the real CLI (and
// without any network or agent binary — see the default-test rule in
// CLAUDE.md). Returns the directory to prepend to PATH.
//
// The stub's form follows the HOST, not the snippet variant being rendered:
// PowerShell is cross-platform, so the Windows cookbook is exercised on a
// Linux runner too, and there a `.cmd` shim would never resolve.
func stubMultica(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	name, body := "multica", "#!/bin/sh\nexit "+strconv.Itoa(exitCode)+"\n"
	if runtime.GOOS == "windows" {
		name, body = "multica.cmd", "@exit /b "+strconv.Itoa(exitCode)+"\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runSnippet executes the snippet in a scratch workdir seeded with reply.md
// and reports the invocation's exit code plus whether reply.md survived.
func runSnippet(t *testing.T, shell string, args []string, stubDir string) (int, bool) {
	t.Helper()
	work := t.TempDir()
	body := filepath.Join(work, "reply.md")
	if err := os.WriteFile(body, []byte("final result"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run snippet: %v", err)
	}
	_, statErr := os.Stat(body)
	return code, statErr == nil
}

// TestCommentReplySnippetPropagatesPostFailure is the regression guard for the
// receipt change: under `--output table` a failed and a successful post both
// write nothing to stdout, so the shell call's exit status is the only
// machine-checkable signal an agent has. A cleanup command that runs
// unconditionally SUCCEEDS after a failed post and overwrites that status with
// 0, which would let an agent end its turn believing an unposted final result
// was delivered — and would delete the body it would need to retry with.
//
// The assertion is deliberately made by EXECUTING the rendered snippet rather
// than matching its text: the property under test is a shell-composition
// behaviour, and a handler-level test (cmd_comment_receipt_test.go) can only
// prove the command itself returns an error, not that the pasted block
// preserves it.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestCommentReplySnippetPropagatesPostFailure(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	const issueID = "11111111-1111-1111-1111-111111111111"
	const triggerID = "22222222-2222-2222-2222-222222222222"

	for _, tc := range []struct {
		name  string
		goos  string
		shell string
		wrap  func(script string) []string
	}{
		{
			name:  "posix/sh",
			goos:  "linux",
			shell: "sh",
			wrap:  func(script string) []string { return []string{"-c", script} },
		},
		{
			// PowerShell is cross-platform, so this exercises the Windows
			// cookbook's $LASTEXITCODE gate wherever pwsh is installed.
			name:  "windows/pwsh",
			goos:  "windows",
			shell: "pwsh",
			wrap:  func(script string) []string { return []string{"-NoProfile", "-Command", script} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.shell); err != nil {
				t.Skipf("%s not available: %v", tc.shell, err)
			}
			runtimeGOOS = tc.goos
			script := strings.Join(snippetLines(t, BuildCommentReplyInstructions("claude", issueID, triggerID, false)), "\n")

			t.Run("failed post keeps the failure and the body", func(t *testing.T) {
				code, bodyKept := runSnippet(t, tc.shell, tc.wrap(script), stubMultica(t, 3))
				if code == 0 {
					t.Errorf("failed post reported success (exit 0); the cleanup masked it\n---\n%s", script)
				}
				if !bodyKept {
					t.Errorf("failed post deleted ./reply.md; the retry would have to regenerate the body\n---\n%s", script)
				}
			})

			t.Run("successful post still cleans up", func(t *testing.T) {
				code, bodyKept := runSnippet(t, tc.shell, tc.wrap(script), stubMultica(t, 0))
				if code != 0 {
					t.Errorf("successful post reported exit %d\n---\n%s", code, script)
				}
				if bodyKept {
					t.Errorf("successful post left ./reply.md behind\n---\n%s", script)
				}
			})
		})
	}
}

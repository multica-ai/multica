//go:build windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// writeTestExecutable is the Windows counterpart to the //go:build unix
// implementation in exec_fixture_unix_test.go. ETXTBSY is a Linux/Unix
// fork-exec race; Windows doesn't have that pathology, so a plain
// os.WriteFile is sufficient — but content across this package's
// fake*Script() fixtures is POSIX `/bin/sh` (shebang lines, `printf`,
// `while IFS= read -r line`, …), and callers pass an extensionless path
// (the Unix convention). Windows has no shebang dispatch, and
// exec.LookPath only resolves an extensionless path by appending each
// PATHEXT suffix (.COM/.EXE/.BAT/.CMD/…) and stat-ing the result — a raw
// sh script written straight to that extensionless path is never found,
// let alone runnable, which is why every backend that spawns one of these
// fixtures failed with "executable file not found" when actually run on a
// native Windows host (previously untested: CI only runs
// ubuntu-latest/macos-latest, so this file compiled but was never
// exercised).
//
// Fix: write the sh content to a sibling ".sh" file, then write a ".bat"
// shim at path+".bat" that execs it through bash (Git for Windows' bundled
// bash.exe — already a prerequisite for working in this repo on Windows).
// exec.LookPath(path) / exec.Command(path, ...) then resolves via
// Windows' extensionless-PATHEXT search straight to path+".bat".
//
// The helper is referenced by claude_test.go / codex_test.go /
// kimi_test.go, so the absence of a Windows impl made
// `go test ./pkg/agent` fail to build on Windows. Lifted from #1719
// (Codex) with attribution.
func writeTestExecutable(tb testing.TB, path string, content []byte) {
	tb.Helper()

	bash, err := exec.LookPath("bash")
	if err != nil {
		tb.Fatalf("writeTestExecutable: bash not found on PATH (required to run POSIX shell-script test fixtures on Windows): %v", err)
	}

	shPath := path + ".sh"
	if err := os.WriteFile(shPath, content, 0o755); err != nil {
		tb.Fatalf("write test executable script %s: %v", shPath, err)
	}

	batPath := path + ".bat"
	batContent := fmt.Sprintf("@echo off\r\n\"%s\" \"%s\" %%*\r\n", bash, shPath)
	if err := os.WriteFile(batPath, []byte(batContent), 0o755); err != nil {
		tb.Fatalf("write test executable shim %s: %v", batPath, err)
	}
}

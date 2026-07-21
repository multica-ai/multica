//go:build windows

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Env contract between the test and the helper process it re-executes as the
// native child of the PowerShell shim.
const (
	shimHelperEnv      = "MULTICA_CURSOR_SHIM_HELPER"
	shimHelperArgvFile = "MULTICA_CURSOR_SHIM_ARGV_FILE"
	shimHelperInFile   = "MULTICA_CURSOR_SHIM_STDIN_FILE"
)

// TestCursorShimHelperProcess is not a test. Re-executed by the fake
// cursor-agent.ps1 below, it stands in for `node.exe index.js` as a real native
// child: it records the argv it was handed, drains stdin, and emits the
// terminal stream-json event. Inert unless the shim env var is set.
func TestCursorShimHelperProcess(t *testing.T) {
	if os.Getenv(shimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}

	// Everything after "--" is what the shim forwarded to us.
	var forwarded []string
	for i, a := range os.Args {
		if a == "--" {
			forwarded = os.Args[i+1:]
			break
		}
	}
	if err := os.WriteFile(os.Getenv(shimHelperArgvFile), []byte(strings.Join(forwarded, "\n")), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
		os.Exit(1)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(shimHelperInFile), stdin, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write stdin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"ok"}`)
	// Exit before the test framework can print PASS/ok into the JSON stream.
	os.Exit(0)
}

// TestCursorExecutePromptSurvivesPowerShellShim is the Windows half of the
// #5649 regression, and it exercises the full production launch chain:
//
//	Go → powershell -File cursor-agent.ps1 → native child
//
// The last hop is the one that matters and the one a shim-only test cannot
// reach. The official cursor-agent.ps1 ends in `& node.exe index.js $args`, so
// the fake ps1 here likewise invokes a *native* executable with $args and lets
// that child record argv and read stdin. That proves both properties the fix
// depends on end to end:
//
//   - the prompt is absent from the argv PowerShell re-serialises onto the
//     child's command line, so there is nothing left to re-tokenise; and
//   - stdin is still inherited by the native child, byte for byte.
//
// It runs against every PowerShell host present, not just the one
// defaultPowerShellLookup would pick, because the hosts differ on exactly the
// mechanism behind this bug: powershell.exe (5.1) and pwsh <= 7.2 default to
// Legacy native argument passing, pwsh >= 7.3 to Standard. A fix that only held
// on the newer host would be no fix at all for the reporter.
func TestCursorExecutePromptSurvivesPowerShellShim(t *testing.T) {
	hosts := availablePowerShellHosts()
	if len(hosts) == 0 {
		t.Skip("no PowerShell host available")
	}
	for _, host := range hosts {
		t.Run(filepath.Base(host), func(t *testing.T) {
			stubPowerShell(t, host, true)
			assertPromptSurvivesShim(t)
		})
	}
}

// availablePowerShellHosts resolves every PowerShell host on PATH so the
// regression can be proven on each independently.
func availablePowerShellHosts() []string {
	var found []string
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			found = append(found, p)
		}
	}
	return found
}

func assertPromptSurvivesShim(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary to use as native child: %v", err)
	}

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	// The .cmd only has to exist and carry the right extension; the rewrite
	// routes around it to the sibling .ps1, which is what actually runs.
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0cursor-agent.ps1\" %*\r\n")

	// Shaped like the official shim: set up, then hand $args to a native child.
	ps1 := fmt.Sprintf(""+
		"$env:%s = '1'\r\n"+
		"$env:%s = '%s'\r\n"+
		"$env:%s = '%s'\r\n"+
		"& '%s' '-test.run=^TestCursorShimHelperProcess$' '--' $args\r\n"+
		"exit $LASTEXITCODE\r\n",
		shimHelperEnv,
		shimHelperArgvFile, argvPath,
		shimHelperInFile, stdinPath,
		self)
	writeFile(t, filepath.Join(dir, "cursor-agent.ps1"), ps1)

	prompt := "Please fix the build.\n" +
		`go build -ldflags "-X main.version=foo -X main.commit=bar" -o bin/server ./cmd/server` + "\n" +
		"Thanks."

	backend, err := New("cursor", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("native child never recorded argv (did the shim reach it?): %v; result=%+v", err, result)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("native child never recorded stdin: %v; result=%+v", err, result)
	}

	// argv as the *native child* received it — past PowerShell's $args
	// re-serialisation, which is where #5649 was introduced.
	for _, a := range strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n") {
		for _, needle := range []string{"-X", "ldflags", "main.version", "Please fix"} {
			if strings.Contains(a, needle) {
				t.Errorf("prompt fragment %q leaked into native child argv element %q", needle, a)
			}
		}
	}
	// The content-free flags must still survive the same hop.
	gotArgv := string(argvRaw)
	for _, want := range []string{"-p", "--output-format", "stream-json", "--yolo"} {
		if !strings.Contains(gotArgv, want) {
			t.Errorf("expected %q to reach the native child; argv was %q", want, gotArgv)
		}
	}

	if string(stdinRaw) != prompt {
		t.Errorf("prompt did not survive Go → PowerShell → native child:\n got  %q\n want %q", string(stdinRaw), prompt)
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}

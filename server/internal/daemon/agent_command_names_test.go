package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestDefaultAgentCommandNamesCoversAllProbes guards the invariant documented
// on defaultAgentCommandNames: the shell-fallback resolver only pre-fetches
// canonical paths for the bare command names in that list, so every agent the
// LoadConfig probe loop tries must appear there. A GUI/Launchpad-started
// daemon does not inherit the interactive shell PATH, so an agent missing from
// this list is undetectable when its binary lives only on the login-shell PATH
// (e.g. an `npm install -g` global). This test parses config.go's probe(...)
// calls so a new probe can't silently diverge from the fallback list.
func TestDefaultAgentCommandNamesCoversAllProbes(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "config.go", nil, 0)
	if err != nil {
		t.Fatalf("parse config.go: %v", err)
	}

	known := make(map[string]bool, len(defaultAgentCommandNames))
	for _, name := range defaultAgentCommandNames {
		known[name] = true
	}

	var missing []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "probe" {
			return true
		}
		// probe(envPathVar, commandName, envModelVar): the command name is the
		// second argument and is the value pre-fetched by the shell fallback.
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		name := lit.Value
		if len(name) >= 2 {
			name = name[1 : len(name)-1] // strip surrounding quotes
		}
		if !known[name] {
			missing = append(missing, name)
		}
		return true
	})

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("probe() command names missing from defaultAgentCommandNames: %v; "+
			"add them so GUI-launched daemons can resolve these agents via the login shell", missing)
	}
}

func TestAgentCLIGuardCoversDefaultCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "agent-cli-command-names.txt"))
	if err != nil {
		t.Fatalf("read agent CLI guard names: %v", err)
	}
	guarded := map[string]bool{}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		if line != strings.TrimSpace(line) {
			t.Fatalf("agent CLI guard name on line %d has surrounding whitespace", lineNumber+1)
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			if !isSafeAgentCLICommandName(line) {
				t.Fatalf("agent CLI guard name on line %d contains unsafe characters: %q", lineNumber+1, line)
			}
			guarded[line] = true
		}
	}
	for _, name := range defaultAgentCommandNames {
		if !guarded[name] {
			t.Errorf("default agent command %q is not covered by the test guard", name)
		}
	}
	if !guarded["qodercli"] {
		t.Error("default qoder command \"qodercli\" is not covered by the test guard")
	}
}

func isSafeAgentCLICommandName(name string) bool {
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return name != ""
}

func TestAgentCLIGuardDetectsSwallowedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "claude --version --token super-secret >/dev/null 2>&1 || true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after a swallowed agent CLI failure: %s", out)
	}
	if !strings.Contains(string(out), "unexpected agent CLI invocation: claude [arguments redacted]") {
		t.Fatalf("guard diagnostic missing invocation: %s", out)
	}
	if strings.Contains(string(out), "super-secret") {
		t.Fatalf("guard diagnostic exposed command arguments: %s", out)
	}
}

func TestAgentCLIGuardFailsClosedWhenSetupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	invalidTempDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidTempDir, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write invalid temp directory fixture: %v", err)
	}
	executedMarker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "printf ran >\"$1\"", "sh", executedMarker)
	cmd.Env = append(os.Environ(), "TMPDIR="+invalidTempDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after setup failure: %s", out)
	}
	if _, statErr := os.Stat(executedMarker); !os.IsNotExist(statErr) {
		t.Fatalf("wrapped command ran after guard setup failure: %v", statErr)
	}
}

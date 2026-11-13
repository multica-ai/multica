package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectRuntimeConfigCodexWindowsWarning(t *testing.T) {
	orig := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = orig })
	runtimeGOOS = "windows"
	dir := t.TempDir()
	if _, err := InjectRuntimeConfig(dir, "codex", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Known Codex Limitation on Windows", "openai/codex#20874", "WSL2"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("generated AGENTS.md lacks %q", want)
		}
	}
	if strings.Contains(string(data), "Assume your tool calls succeed") {
		t.Error("missing output must not be treated as proof of success")
	}
}

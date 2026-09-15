package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated Windows brief is the quick-create agent's recovery policy.
// A blocked .NET writer must not abort before it can try a permitted writer.
func TestQuickCreateWindowsDescriptionWriteRecovery(t *testing.T) {
	previous := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = previous })
	for _, host := range []string{"windows", "linux"} {
		t.Run(host, func(t *testing.T) {
			runtimeGOOS = host
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, "codex", TaskContextForEnv{QuickCreatePrompt: "create an issue"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			brief := string(data)
			if strings.Contains(brief, "treat a failed write as fatal") {
				t.Fatal("quick-create still aborts before attempting a compatible file writer")
			}
			if !strings.Contains(brief, "never run `--description-file` against a file whose write did not succeed") {
				t.Fatal("file-write recovery must retain the guard against stale description files")
			}
			if host == "windows" {
				for _, want := range []string{"ConstrainedLanguage", "file-write tool", "Set-Content -LiteralPath", "-Encoding utf8", "-ErrorAction Stop", "retry the write", "do not disable the sandbox"} {
					if !strings.Contains(brief, want) {
						t.Errorf("Windows quick-create brief missing %q", want)
					}
				}
			} else if strings.Contains(brief, "ConstrainedLanguage") {
				t.Fatal("PowerShell-specific write guidance leaked into the Linux brief")
			}
		})
	}
}

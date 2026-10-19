package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMuseLocalSkillsUseConfigAndUniversalRoots(t *testing.T) {
	for _, customConfig := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "xdg"}[customConfig], func(t *testing.T) {
			home := t.TempDir()
			redirectTestHome(t, home)
			configDir := filepath.Join(home, ".config")
			t.Setenv("XDG_CONFIG_HOME", "")
			if customConfig {
				configDir = t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", configDir)
			}
			writeTestLocalSkill(t, filepath.Join(configDir, "muse", "skills"), "personal", map[string]string{
				"SKILL.md": "---\nname: Personal\ndescription: Muse personal skill\n---\n",
			})
			writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "shared", map[string]string{
				"SKILL.md": "---\nname: Shared\ndescription: Cross-agent skill\n---\n",
			})
			skills, supported, err := listRuntimeLocalSkills("muse")
			if err != nil || !supported || len(skills) != 2 {
				t.Fatalf("supported=%v skills=%v err=%v", supported, skills, err)
			}
			_, supported, err = loadRuntimeLocalSkillBundle("muse", "personal")
			if err != nil || !supported {
				t.Fatalf("personal skill cannot be imported: %v", err)
			}
		})
	}
}

func TestMuseDetectedThroughLoginShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX login shell")
	}
	home := t.TempDir()
	redirectTestHome(t, home)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "muse")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'Muse Code 1.4.10\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(t.TempDir(), "shell.rc")
	if err := os.WriteFile(rc, []byte("export PATH=\""+binDir+":$PATH\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", rc)
	t.Setenv("MULTICA_DAEMON_ID", "11111111-1111-1111-1111-111111111111")
	pinNonCodexAgentsToMissingPaths(t)
	t.Setenv("MULTICA_CODEX_PATH", filepath.Join(t.TempDir(), "missing-codex"))
	t.Setenv("MULTICA_MUSE_PATH", "")
	cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := cfg.Agents["muse"]; !ok || got.Path != canonicalExecutablePath(bin) {
		t.Fatalf("login-shell Muse was not detected: %+v", cfg.Agents)
	}
}

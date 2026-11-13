package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAmpDiscoveryAndConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "PATH", true: "override"}[override], func(t *testing.T) {
			bin := stageFakeAgent(t)
			path := filepath.Join(bin, "amp")
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELL", "/usr/bin/fish")
			t.Setenv("MULTICA_AMP_PATH", "")
			if override {
				t.Setenv("MULTICA_AMP_PATH", path)
			}
			t.Setenv("MULTICA_AMP_MODEL", "high")
			t.Setenv("MULTICA_AMP_ARGS", "--features fast")
			cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			wantPath, _ := filepath.EvalSymlinks(path)
			entry, ok := cfg.Agents["amp"]
			gotPath, _ := filepath.EvalSymlinks(entry.Path)
			if !ok || gotPath != wantPath || entry.Model != "high" {
				t.Fatalf("Amp not discovered: %+v", cfg.Agents)
			}
			args := defaultArgsForProvider(cfg, "amp")
			if strings.Join(args, " ") != "--features fast" {
				t.Fatalf("args = %v", args)
			}
			args[0] = "changed"
			if cfg.AmpArgs[0] != "--features" {
				t.Fatal("caller mutated daemon defaults")
			}
		})
	}
}

func TestAmpLocalSkills(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	writeTestLocalSkill(t, filepath.Join(home, ".config", "amp", "skills"), "review", map[string]string{"SKILL.md": "---\nname: Review\ndescription: Review code\n---\n"})
	skills, supported, err := listRuntimeLocalSkills("amp")
	if err != nil || !supported || len(skills) != 1 || skills[0].SourcePath != "~/.config/amp/skills/review" {
		t.Fatalf("Amp local skills = %+v, %v, %v", skills, supported, err)
	}
}

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDroidDiscoveryAndConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "PATH", true: "override"}[override], func(t *testing.T) {
			bin := stageFakeAgent(t)
			path := filepath.Join(bin, "droid")
			if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELL", "/usr/bin/fish")
			t.Setenv("MULTICA_DROID_PATH", "")
			if override {
				t.Setenv("MULTICA_DROID_PATH", path)
			}
			t.Setenv("MULTICA_DROID_MODEL", "custom:test")
			t.Setenv("MULTICA_DROID_ARGS", "--reasoning-effort high")
			cfg, err := LoadConfig(Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			wantPath, _ := filepath.EvalSymlinks(path)
			entry, ok := cfg.Agents["droid"]
			gotPath, _ := filepath.EvalSymlinks(entry.Path)
			if !ok || gotPath != wantPath || entry.Model != "custom:test" {
				t.Fatalf("Droid not discovered: %+v", cfg.Agents)
			}
			args := defaultArgsForProvider(cfg, "droid")
			if strings.Join(args, " ") != "--reasoning-effort high" {
				t.Fatalf("args = %v", args)
			}
			args[0] = "changed"
			if cfg.DroidArgs[0] != "--reasoning-effort" {
				t.Fatal("caller mutated daemon defaults")
			}
		})
	}
}

func TestDroidLocalSkills(t *testing.T) {
	home := t.TempDir()
	redirectTestHome(t, home)
	writeTestLocalSkill(t, filepath.Join(home, ".factory", "skills"), "review", map[string]string{"SKILL.md": "---\nname: Review\ndescription: Review code\n---\n"})
	skills, supported, err := listRuntimeLocalSkills("droid")
	if err != nil || !supported || len(skills) != 1 || skills[0].SourcePath != "~/.factory/skills/review" {
		t.Fatalf("Droid local skills = %+v, %v, %v", skills, supported, err)
	}
}

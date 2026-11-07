package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiscoverOmpSkillsUsesRuntimePrefixWithoutPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "argv")
	t.Setenv("MULTICA_TEST_OMP_ARGS", capture)
	path := filepath.Join(dir, "profile", "SKILL.md")
	snapshot, _ := json.Marshal(map[string]any{"skills": []map[string]string{{
		"source": "skill", "name": "skill:profile-only", "description": "Selected profile", "path": path,
	}}})
	t.Setenv("MULTICA_TEST_OMP_SNAPSHOT", string(snapshot))
	binary := filepath.Join(dir, "wrapper")
	writeTestExecutable(t, binary, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$MULTICA_TEST_OMP_ARGS"
cat > "$MULTICA_TEST_OMP_ARGS.stdin"
pwd > "$MULTICA_TEST_OMP_ARGS.cwd"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) cat "$2" > "$MULTICA_TEST_OMP_ARGS.config" ;;
  esac
  shift
done
printf '%s' "$MULTICA_TEST_OMP_SNAPSHOT" > "$MULTICA_OMP_SKILLS_OUTPUT"
`))
	prefix := []string{"launch", "--profile", "review-a", "--no-extensions"}
	skills, err := DiscoverOmpSkills(t.Context(), NewCommand(binary, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(skills, []OmpSkill{{Name: "profile-only", Description: "Selected profile", Path: path}}) {
		t.Fatalf("skills=%+v", skills)
	}
	raw, _ := os.ReadFile(capture)
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if !reflect.DeepEqual(args[:len(prefix)], prefix) {
		t.Fatalf("prefix lost: %v", args)
	}
	for _, flag := range []string{"--mode", "rpc", "--no-session", "--extension"} {
		if !strings.Contains("\n"+string(raw), "\n"+flag+"\n") {
			t.Errorf("missing %s: %v", flag, args)
		}
	}
	input, _ := os.ReadFile(capture + ".stdin")
	if len(input) != 0 {
		t.Fatalf("probe sent input: %q", input)
	}
	config, _ := os.ReadFile(capture + ".config")
	if string(config) != "skills:\n  enableSkillCommands: true\n" {
		t.Fatalf("enumeration overlay changed skill filtering: %s", config)
	}
	cwd, _ := os.ReadFile(capture + ".cwd")
	if _, err := os.Stat(strings.TrimSpace(string(cwd))); !os.IsNotExist(err) {
		t.Fatalf("probe working directory was not cleaned: %s (%v)", cwd, err)
	}
}

func TestDiscoverOmpSkillsFailureAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	for _, tc := range []struct{ name, script string }{
		{"exit", "exit 1"},
		{"no-inventory", "exit 0"},
		{"invalid-inventory", `printf '{}' > "$MULTICA_OMP_SKILLS_OUTPUT"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "omp")
			writeTestExecutable(t, binary, []byte("#!/bin/sh\n"+tc.script+"\n"))
			if _, err := DiscoverOmpSkills(t.Context(), NewCommand(binary, nil)); err == nil {
				t.Fatal("expected a discovery error, not an installed-file fallback")
			}
		})
	}
	binary := filepath.Join(t.TempDir(), "slow-omp")
	writeTestExecutable(t, binary, []byte("#!/bin/sh\nexec sleep 30\n"))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := DiscoverOmpSkills(ctx, NewCommand(binary, nil)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestParseOmpSkills(t *testing.T) {
	for _, data := range []string{`{}`, `{"skills":null}`, `not json`, `{"skills":[{"source":"skill","name":"skill:x","path":"relative/SKILL.md"}]}`} {
		if _, err := parseOmpSkills([]byte(data)); err == nil {
			t.Errorf("accepted invalid snapshot %s", data)
		}
	}
	empty, err := parseOmpSkills([]byte(`{"skills":[]}`))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("valid empty roster: %v %v", empty, err)
	}
	path := filepath.Join(t.TempDir(), "SKILL.md")
	entry := map[string]string{"source": "skill", "name": "skill:plugin/review", "path": path}
	data, _ := json.Marshal(map[string]any{"skills": []any{entry, entry}})
	skills, err := parseOmpSkills(data)
	if err != nil || len(skills) != 1 || skills[0].Name != "plugin/review" {
		t.Fatalf("namespaced/deduplicated roster: %+v %v", skills, err)
	}
}

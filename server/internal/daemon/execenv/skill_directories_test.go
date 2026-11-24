package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWrittenSkillDirectoriesIncludeEveryCollision(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "a-b"), 0755); err != nil {
		t.Fatal(err)
	}
	skills := []SkillContextForEnv{
		{ID: "one", Name: "A B", Content: "---\ndisable-model-invocation: true\n---\nfirst"},
		{ID: "two", Name: "A-B", Content: "second"},
	}
	installed := map[string]string{}
	if err := writeSkillFilesTracked(root, skills, nil, installed); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"one": filepath.Join(root, "a-b-multica"), "two": filepath.Join(root, "a-b-multica-multica")}
	if !reflect.DeepEqual(installed, want) {
		t.Fatalf("installed=%v want=%v", installed, want)
	}
	for id, dir := range installed {
		body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil || !strings.Contains(string(body), "name: "+filepath.Base(dir)) {
			t.Fatalf("%s: %s, %v", id, body, err)
		}
	}
}

func TestPrepareAndReuseReturnSkillDirectories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("HERMES_HOME", t.TempDir())
	task := TaskContextForEnv{ChatSessionID: "chat", AgentSkills: []SkillContextForEnv{{ID: "selected", Name: "Review Code", Content: "Use references/file.txt"}}}
	for _, provider := range []string{"amp", "codex", "hermes"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			sourceHome := t.TempDir()
			env, err := Prepare(PrepareParams{WorkspacesRoot: root, WorkspaceID: "workspace", TaskID: "task-skill-directories", Provider: provider, HermesSourceHome: sourceHome, Task: task}, testLogger())
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(env.WorkDir, ".agents", "skills", "review-code")
			switch provider {
			case "codex":
				want = filepath.Join(env.RootDir, "codex-home", "skills", "review-code")
				if filepath.Dir(filepath.Dir(want)) != env.CodexHome {
					t.Fatalf("Codex skill is outside the private home: %+v", env)
				}
			case "hermes":
				want = filepath.Join(env.RootDir, "hermes-home", "skills", "review-code")
				if filepath.Dir(filepath.Dir(want)) != env.HermesHome {
					t.Fatalf("Hermes skill is outside the overlay: %+v", env)
				}
			}
			if env.SkillDirectories["selected"] != want {
				t.Fatalf("Prepare directories=%v", env.SkillDirectories)
			}
			// The isolated preparer returns JSON, not the in-process slice/map.
			encoded, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Environment
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.SkillDirectories["selected"] != want {
				t.Fatal("helper response lost directory")
			}
			for _, stage := range []string{"Prepare", "Reuse"} {
				if stage == "Reuse" {
					env = Reuse(ReuseParams{WorkspacesRoot: root, WorkDir: env.WorkDir, Provider: provider, HermesSourceHome: sourceHome, Task: task}, testLogger())
				}
				if env == nil || env.SkillDirectories["selected"] != want {
					t.Fatalf("%s directories=%+v", stage, env)
				}
				body, err := os.ReadFile(filepath.Join(env.SkillDirectories["selected"], "SKILL.md"))
				if err != nil || !strings.Contains(string(body), "Use references/file.txt") {
					t.Fatalf("%s mapping does not locate the selected body: %s, %v", stage, body, err)
				}
				prepared := ApplyPreparedSkillNames(task, env.PreparedSkillNames)
				receipt := env.SkillReceipts(provider, prepared.AgentSkills)[0]
				if !receipt.Available || receipt.Bytes != len(body) || receipt.Path != SkillDeliveryLocation(env.WorkDir, provider)+"/review-code/SKILL.md" {
					t.Fatalf("%s receipt does not match the selected directory: %+v", stage, receipt)
				}
			}
		})
	}
}

func TestSkillBodyStripsOnlyFrontmatter(t *testing.T) {
	for _, input := range []string{"---\nname: x\n---\nbody\n---\ntail", "---\r\nname: x\r\n---\r\nbody\n---\ntail", "body\n---\ntail"} {
		if got := SkillBody(input); strings.TrimSpace(got) != "body\n---\ntail" {
			t.Fatalf("SkillBody(%q)=%q", input, got)
		}
	}
}

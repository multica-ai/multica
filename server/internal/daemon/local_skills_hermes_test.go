package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestHermesAgentSkillCatalogIsolation(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	hermesHome := filepath.Join(home, "hermes")
	t.Setenv("HERMES_HOME", hermesHome)
	for _, profile := range []string{"alpha", "beta"} {
		root := filepath.Join(hermesHome, "profiles", profile)
		writeTestLocalSkill(t, filepath.Join(root, "skills"), profile, map[string]string{"SKILL.md": "---\nname: " + profile + "\n---\nProfile skill"})
		writeTestLocalSkill(t, filepath.Join(root, "external"), "external-"+profile, map[string]string{"SKILL.md": "---\nname: external-" + profile + "\n---\nExternal skill"})
		// A same-key skill pins the precedence of the task overlay's external dirs.
		writeTestLocalSkill(t, filepath.Join(root, "external"), profile, map[string]string{"SKILL.md": "---\nname: External winner\n---\nExternal wins"})
		if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("skills:\n  external_dirs: ['${HERMES_HOME}/external']\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestLocalSkill(t, filepath.Join(hermesHome, "skills"), "default-only", map[string]string{"SKILL.md": "---\nname: Default\n---\nDefault skill"})

	cases := []struct {
		name  string
		scope *protocol.LocalSkillAgentScope
		fixed []string
		want  []string
	}{
		{"alpha", &protocol.LocalSkillAgentScope{AgentID: "a", CustomArgs: []string{"-p", "alpha"}}, nil, []string{"alpha", "external-alpha"}},
		{"beta", &protocol.LocalSkillAgentScope{AgentID: "b", CustomArgs: []string{"--profile=beta"}}, nil, []string{"beta", "external-beta"}},
		{"fixed prefix wins", &protocol.LocalSkillAgentScope{AgentID: "a", CustomArgs: []string{"-p", "beta"}}, []string{"-p", "alpha"}, []string{"alpha", "external-alpha"}},
		{"custom home", &protocol.LocalSkillAgentScope{AgentID: "b", CustomEnv: map[string]string{"HERMES_HOME": filepath.Join(hermesHome, "profiles", "beta")}}, nil, []string{"beta", "external-beta"}},
		{"runtime default import scope", nil, nil, []string{"default-only"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots, supported, err := localSkillRootsForScope("hermes", tc.scope, tc.fixed)
			if err != nil || !supported {
				t.Fatalf("roots: supported=%v err=%v", supported, err)
			}
			skills, err := listRuntimeLocalSkillsFromRoots("hermes", roots)
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, s := range skills {
				keys = append(keys, s.Key)
			}
			if !reflect.DeepEqual(keys, tc.want) {
				t.Fatalf("keys=%v want=%v", keys, tc.want)
			}
			if tc.scope != nil && skills[0].Name != "External winner" {
				t.Fatalf("wrong precedence: %+v", skills)
			}
		})
	}
	// Import remains explicitly runtime-default, never whichever agent was last listed.
	bundle, supported, err := loadRuntimeLocalSkillBundle("hermes", "default-only")
	if err != nil || !supported || bundle == nil {
		t.Fatalf("default import: %v %v %v", bundle, supported, err)
	}
	if _, _, err := loadRuntimeLocalSkillBundle("hermes", "alpha"); err == nil {
		t.Fatal("runtime-default import read a different agent's profile")
	}
	for _, profile := range []string{"absent", "../beta", ""} {
		_, _, err := localSkillRootsForScope("hermes", &protocol.LocalSkillAgentScope{AgentID: "a", CustomArgs: []string{"--profile=" + profile}}, nil)
		if err == nil {
			t.Fatalf("invalid/missing profile %q silently fell back", profile)
		}
	}
}

func TestHandleLocalSkillListCarriesAgentScope(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	t.Setenv("HERMES_HOME", filepath.Join(home, "hermes"))
	root := filepath.Join(home, "hermes", "profiles", "alpha", "skills")
	writeTestLocalSkill(t, root, "profile-only", map[string]string{"SKILL.md": "---\nname: Profile Only\n---\nProfile skill"})
	var got struct {
		AgentID string                     `json:"agent_id"`
		Skills  []runtimeLocalSkillSummary `json:"skills"`
		Status  string                     `json:"status"`
	}
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	d.handleLocalSkillList(context.Background(), Runtime{ID: "runtime", Provider: "hermes"}, PendingLocalSkills{
		ID: "request", AgentScope: &protocol.LocalSkillAgentScope{AgentID: "alpha-agent", CustomArgs: []string{"-p", "alpha"}},
	})
	if got.Status != "completed" || got.AgentID != "alpha-agent" || len(got.Skills) != 1 || got.Skills[0].Key != "profile-only" {
		t.Fatalf("unexpected catalog: %+v", got)
	}
}

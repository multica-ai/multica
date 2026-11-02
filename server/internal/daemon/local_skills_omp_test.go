package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestOmpLocalSkillsListAndImportUseSelectedProfile(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	// The shared copy must neither leak into the roster nor shadow the
	// same-named skill OMP selected from its profile or custom directory.
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "review", map[string]string{"SKILL.md": "shared copy"})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "excluded", map[string]string{"SKILL.md": "excluded copy"})
	profileA := writeTestLocalSkill(t, filepath.Join(home, ".omp", "profiles", "a", "agent", "skills"), "review", map[string]string{
		"SKILL.md": "profile A", "references/help.txt": "A reference",
	})
	profileB := writeTestLocalSkill(t, filepath.Join(home, "custom"), "review", map[string]string{"SKILL.md": "profile B"})
	var report struct {
		Status string                     `json:"status"`
		Skills []runtimeLocalSkillSummary `json:"skills"`
		Skill  *runtimeLocalSkillBundle   `json:"skill"`
	}
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	d.runtimeIndex = map[string]Runtime{}
	d.profileLaunchSpecs = map[string]profileLaunchSpec{}
	previous := discoverOmpSkills
	t.Cleanup(func() { discoverOmpSkills = previous })
	for _, tc := range []struct{ profile, dir, content string }{{"a", profileA, "profile A"}, {"b", profileB, "profile B"}} {
		t.Run(tc.profile, func(t *testing.T) {
			rt := Runtime{ID: "runtime-" + tc.profile, Provider: "omp", ProfileID: tc.profile}
			d.runtimeIndex[rt.ID] = rt
			d.profileLaunchSpecs[tc.profile] = profileLaunchSpec{path: "/custom/omp-wrapper", fixedArgs: []string{"launch", "--profile", tc.profile}}
			discoverOmpSkills = func(_ context.Context, command agent.Command) ([]agent.OmpSkill, error) {
				if command.Path != "/custom/omp-wrapper" || !reflect.DeepEqual(command.Prefix, []string{"launch", "--profile", tc.profile}) {
					t.Fatalf("wrong runtime command: %+v", command)
				}
				return []agent.OmpSkill{{Name: "review", Description: "Resolved skill", Path: filepath.Join(tc.dir, "SKILL.md")}}, nil
			}
			d.handleLocalSkillList(t.Context(), rt, "list-request")
			if report.Status != "completed" || len(report.Skills) != 1 || report.Skills[0].Key != "review" || report.Skills[0].SourcePath != relativizeHomePath(tc.dir) {
				t.Fatalf("wrong inventory: %+v", report)
			}
			d.handleLocalSkillImport(t.Context(), rt, PendingLocalSkillImport{ID: "import-request", SkillKey: "review"})
			if report.Status != "completed" || report.Skill == nil || report.Skill.Content != tc.content || report.Skill.SourcePath != report.Skills[0].SourcePath {
				t.Fatalf("import did not match listed profile copy: %+v", report.Skill)
			}
			if tc.profile == "a" && (report.Skills[0].FileCount != 2 || len(report.Skill.Files) != 1 || report.Skill.Files[0].Content != "A reference") {
				t.Fatalf("supporting files lost: %+v", report.Skill)
			}
			if _, _, err := d.loadLocalSkillBundle(t.Context(), rt, "excluded"); err == nil {
				t.Fatal("import accepted a shared skill excluded by the profile")
			}
		})
	}
}

func TestOmpLocalSkillsDoNotFallBackOnDiscoveryFailure(t *testing.T) {
	setTestUserHome(t, t.TempDir())
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		_ = json.NewDecoder(r.Body).Decode(&report)
		if report["status"] != "failed" || !strings.Contains(report["error"].(string), "inventory unavailable") {
			t.Errorf("discovery failure was not reported: %v", report)
		}
		w.WriteHeader(http.StatusOK)
	})
	rt := Runtime{ID: "runtime", Provider: "omp", ProfileID: "profile"}
	d.runtimeIndex = map[string]Runtime{rt.ID: rt}
	d.profileLaunchSpecs = map[string]profileLaunchSpec{rt.ProfileID: {path: "/custom/omp"}}
	previous := discoverOmpSkills
	t.Cleanup(func() { discoverOmpSkills = previous })
	discoverOmpSkills = func(context.Context, agent.Command) ([]agent.OmpSkill, error) {
		return nil, errors.New("inventory unavailable")
	}
	d.handleLocalSkillList(t.Context(), rt, "list")
	d.handleLocalSkillImport(t.Context(), rt, PendingLocalSkillImport{ID: "import", SkillKey: "review"})
	delete(d.profileLaunchSpecs, rt.ProfileID)
	if _, err := d.ompSkills(t.Context(), rt); err == nil {
		t.Fatal("missing custom command fell back to the default runtime")
	}
}

func TestOmpLocalSkillsUseConfiguredBuiltinBinary(t *testing.T) {
	d := freshDaemon("http://unused.invalid")
	binary := fakeExecutable(t, "omp")
	d.cfg.Agents = map[string]AgentEntry{"omp": {Path: binary}}
	previous := discoverOmpSkills
	t.Cleanup(func() { discoverOmpSkills = previous })
	discoverOmpSkills = func(_ context.Context, command agent.Command) ([]agent.OmpSkill, error) {
		if command.Path != binary || len(command.Prefix) != 0 {
			t.Fatalf("wrong built-in command: %+v", command)
		}
		return []agent.OmpSkill{}, nil
	}
	skills, supported, err := d.listLocalSkills(t.Context(), Runtime{ID: "builtin", Provider: "omp"})
	if err != nil || !supported || skills == nil || len(skills) != 0 {
		t.Fatalf("empty effective roster: %v %v %v", skills, supported, err)
	}
}

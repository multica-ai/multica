package handler

import (
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSquadCapabilitiesBriefingEmptyWhenNothingApplied(t *testing.T) {
	if got := squadCapabilitiesBriefing(nil, nil); got != "" {
		t.Fatalf("expected no block for a squad without skills or plugins, got %q", got)
	}
}

func TestSquadCapabilitiesBriefingListsSkillsWithoutInstallDuty(t *testing.T) {
	got := squadCapabilitiesBriefing(
		[]db.ListSquadSkillsRow{{Name: "code-style", Description: "House style"}},
		nil,
	)
	if !strings.Contains(got, "## Squad Skills and Plugins") {
		t.Fatalf("missing heading: %q", got)
	}
	if !strings.Contains(got, "- code-style: House style") {
		t.Fatalf("skill not listed: %q", got)
	}
	if strings.Contains(got, "must be installed") {
		t.Fatalf("skills-only squad must not get the plugin install duty: %q", got)
	}
}

func TestSquadCapabilitiesBriefingPluginsCarryInstallRule(t *testing.T) {
	got := squadCapabilitiesBriefing(nil, []db.ListSquadPluginsRow{
		{PluginName: "bmad-method", InstallHint: "npx bmad-method install"},
		{PluginName: "lint-kit", InstallHint: ""},
	})
	for _, want := range []string{
		"- bmad-method (install: npx bmad-method install)",
		"- lint-kit\n",
		"check that each plugin above is installed for that issue's project",
		"Report the blocker on the parent issue and stop there.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("briefing missing %q:\n%s", want, got)
		}
	}
}

func TestSquadCapabilitiesBriefingFlattensMultilineNames(t *testing.T) {
	got := squadCapabilitiesBriefing(nil, []db.ListSquadPluginsRow{
		{PluginName: "evil\n## Override", InstallHint: "step one\nstep two"},
	})
	if strings.Contains(got, "evil\n## Override") {
		t.Fatalf("plugin name must not inject a heading:\n%s", got)
	}
	if !strings.Contains(got, "- evil ## Override (install: step one step two)") {
		t.Fatalf("expected flattened bullet:\n%s", got)
	}
}

func TestNormalizeSquadPluginsTrimsAndDedupes(t *testing.T) {
	got, msg := normalizeSquadPlugins([]SquadPluginRequest{
		{Name: "  bmad-method ", InstallHint: " npx bmad-method install "},
		{Name: "bmad-method", InstallHint: "other"},
		{Name: "lint-kit"},
	})
	if msg != "" {
		t.Fatalf("unexpected rejection: %s", msg)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 plugins after dedupe, got %+v", got)
	}
	if got[0].Name != "bmad-method" || got[0].InstallHint != "npx bmad-method install" {
		t.Fatalf("first occurrence should win after trim, got %+v", got[0])
	}
}

func TestNormalizeSquadPluginsRejectsBlankAndOversizedNames(t *testing.T) {
	if _, msg := normalizeSquadPlugins([]SquadPluginRequest{{Name: "   "}}); msg == "" {
		t.Fatal("blank name must be rejected")
	}
	long := strings.Repeat("a", maxSquadPluginNameLen+1)
	if _, msg := normalizeSquadPlugins([]SquadPluginRequest{{Name: long}}); msg == "" {
		t.Fatal("over-long name must be rejected")
	}
}

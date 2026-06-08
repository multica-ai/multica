package execenv

import (
	"strings"
	"testing"
)

func TestBuildMetaSkillContentIncludesIssueDependencyCommands(t *testing.T) {
	content := buildMetaSkillContent("codex", TaskContextForEnv{IssueID: "issue-123"})
	if !strings.Contains(content, "multica issue dependency list <issue-id> --output json") {
		t.Fatalf("expected dependency list command in runtime config, got: %s", content)
	}
	if !strings.Contains(content, "multica issue dependency add <issue-id> --depends-on <issue-id> [--type blocked_by]") {
		t.Fatalf("expected dependency add command in runtime config, got: %s", content)
	}
}

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSkillUsageFromTranscriptCountsExplicitReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"tool_call","input":{"cmd":"cat /opt/codex-home/skills/test-driven-development/SKILL.md"}}
{"type":"tool_result","output":"/opt/codex-home/skills/test-driven-development/SKILL.md"}
{"type":"tool_call","input":{"cmd":"multica skill get 11111111-1111-1111-1111-111111111111 --output json"}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := extractSkillUsageFromTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "test-driven-development" || got[0].Count != 1 || got[1].ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected skill usage: %#v", got)
	}
}

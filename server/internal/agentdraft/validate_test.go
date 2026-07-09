package agentdraft

import (
	"strings"
	"testing"
)

const testAgentID = "00000000-0000-0000-0000-000000000123"

func TestNormalizeAgentDraftResultValid(t *testing.T) {
	raw := []byte(`{
		"agent_id":" 00000000-0000-0000-0000-000000000123 ",
		"name":" Frontend Maintainer ",
		"summary":" Created a focused frontend agent. ",
		"skill_source_urls":["https://github.com/vercel-labs/agent-skills/tree/main/skills/react-best-practices","https://github.com/vercel-labs/agent-skills/tree/main/skills/react-best-practices"]
	}`)

	normalized, result, err := NormalizeAgentDraftResult(raw)
	if err != nil {
		t.Fatalf("NormalizeAgentDraftResult returned error: %v", err)
	}
	if result.AgentID != testAgentID {
		t.Fatalf("agent id was not trimmed/kept: %q", result.AgentID)
	}
	if result.Name != "Frontend Maintainer" {
		t.Fatalf("name was not trimmed: %q", result.Name)
	}
	if len(result.SkillSourceURLs) != 1 {
		t.Fatalf("expected duplicate skill URL to be collapsed, got %d", len(result.SkillSourceURLs))
	}
	if !strings.Contains(string(normalized), `"agent_id":"00000000-0000-0000-0000-000000000123"`) {
		t.Fatalf("normalized JSON did not contain normalized agent id: %s", normalized)
	}
}

func TestNormalizeAgentDraftResultRejectsInvalidAgentID(t *testing.T) {
	_, _, err := NormalizeAgentDraftResult([]byte(`{"agent_id":"not-a-uuid","name":"A","summary":"B"}`))
	if err == nil {
		t.Fatal("expected invalid agent_id to fail")
	}
}

func TestNormalizeAgentDraftResultRejectsUnknownSkillURL(t *testing.T) {
	_, _, err := NormalizeAgentDraftResult([]byte(`{
		"agent_id":"00000000-0000-0000-0000-000000000123",
		"name":"A",
		"summary":"B",
		"skill_source_urls":["https://example.com/skill"]
	}`))
	if err == nil {
		t.Fatal("expected unknown skill URL to fail")
	}
}

func TestNormalizeAgentDraftResultRejectsUnknownFields(t *testing.T) {
	_, _, err := NormalizeAgentDraftResult([]byte(`{
		"agent_id":"00000000-0000-0000-0000-000000000123",
		"name":"A",
		"summary":"B",
		"extra":true
	}`))
	if err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

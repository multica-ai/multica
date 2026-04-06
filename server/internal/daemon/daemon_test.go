package daemon

import (
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"multica issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestResolveModelAndEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		entry        AgentEntry
		agentData    *AgentData
		wantModel    string
		wantEffort   string
	}{
		{
			name:       "no agent data uses entry model, empty effort",
			entry:      AgentEntry{Model: "claude-sonnet-4-6"},
			agentData:  nil,
			wantModel:  "claude-sonnet-4-6",
			wantEffort: "",
		},
		{
			name:       "agent data effort is passed through",
			entry:      AgentEntry{Model: "claude-sonnet-4-6"},
			agentData:  &AgentData{Effort: "high"},
			wantModel:  "claude-sonnet-4-6",
			wantEffort: "high",
		},
		{
			name:       "agent data model overrides entry model",
			entry:      AgentEntry{Model: "claude-sonnet-4-6"},
			agentData:  &AgentData{Model: "claude-opus-4-6", Effort: "max"},
			wantModel:  "claude-opus-4-6",
			wantEffort: "max",
		},
		{
			name:       "empty agent model keeps entry model",
			entry:      AgentEntry{Model: "claude-haiku-4-5"},
			agentData:  &AgentData{Effort: "low"},
			wantModel:  "claude-haiku-4-5",
			wantEffort: "low",
		},
		{
			name:       "all effort values accepted",
			entry:      AgentEntry{},
			agentData:  &AgentData{Effort: "medium"},
			wantModel:  "",
			wantEffort: "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotModel, gotEffort := resolveModelAndEffort(tt.entry, tt.agentData)
			if gotModel != tt.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tt.wantModel)
			}
			if gotEffort != tt.wantEffort {
				t.Errorf("effort = %q, want %q", gotEffort, tt.wantEffort)
			}
		})
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

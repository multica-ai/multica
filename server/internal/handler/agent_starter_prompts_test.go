package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestNormaliseAgentStarterPrompts(t *testing.T) {
	t.Run("trims complete prompts", func(t *testing.T) {
		got, err := normaliseAgentStarterPrompts([]AgentStarterPrompt{{
			Label:  "  Review a PR  ",
			Prompt: "  Review the open pull request.  ",
		}})
		if err != nil {
			t.Fatalf("normaliseAgentStarterPrompts() error = %v", err)
		}
		want := []AgentStarterPrompt{{Label: "Review a PR", Prompt: "Review the open pull request."}}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("normaliseAgentStarterPrompts() = %#v, want %#v", got, want)
		}
	})

	for name, prompts := range map[string][]AgentStarterPrompt{
		"too many": {
			{Label: "One", Prompt: "One"},
			{Label: "Two", Prompt: "Two"},
			{Label: "Three", Prompt: "Three"},
			{Label: "Four", Prompt: "Four"},
		},
		"blank label":  {{Label: " ", Prompt: "Prompt"}},
		"blank prompt": {{Label: "Label", Prompt: " "}},
		"long label":   {{Label: strings.Repeat("a", maxAgentStarterPromptLabel+1), Prompt: "Prompt"}},
		"long prompt":  {{Label: "Label", Prompt: strings.Repeat("a", maxAgentStarterPromptLength+1)}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normaliseAgentStarterPrompts(prompts); err == nil {
				t.Fatal("normaliseAgentStarterPrompts() error = nil, want validation error")
			}
		})
	}
}

func TestAgentStarterPromptsRoundTrip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	var created AgentResponse
	testutil.Call(t, testHandler.CreateAgent, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":       fmt.Sprintf("starter-prompts-%d", time.Now().UnixNano()),
		"runtime_id": handlerTestRuntimeID(t),
		"starter_prompts": []map[string]string{{
			"label":  "  Review a PR  ",
			"prompt": "  Review the most relevant open pull request.  ",
		}},
	})).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE id = $1`, created.ID)
	if len(created.StarterPrompts) != 1 ||
		created.StarterPrompts[0].Label != "Review a PR" ||
		created.StarterPrompts[0].Prompt != "Review the most relevant open pull request." {
		t.Fatalf("created starter_prompts = %#v", created.StarterPrompts)
	}

	var preserved AgentResponse
	testutil.Call(t, testHandler.UpdateAgent, withURLParam(
		newRequest(http.MethodPut, "/api/agents/"+created.ID, map[string]any{
			"description": "starter prompts unchanged",
		}),
		"id",
		created.ID,
	)).Want(http.StatusOK).JSON(&preserved)
	if len(preserved.StarterPrompts) != 1 {
		t.Fatalf("omitted update starter_prompts = %#v, want preserved prompt", preserved.StarterPrompts)
	}

	var clearedAgent AgentResponse
	testutil.Call(t, testHandler.UpdateAgent, withURLParam(
		newRequest(http.MethodPut, "/api/agents/"+created.ID, map[string]any{
			"starter_prompts": []AgentStarterPrompt{},
		}),
		"id",
		created.ID,
	)).Want(http.StatusOK).JSON(&clearedAgent)
	if len(clearedAgent.StarterPrompts) != 0 {
		t.Fatalf("cleared starter_prompts = %#v, want empty", clearedAgent.StarterPrompts)
	}
}

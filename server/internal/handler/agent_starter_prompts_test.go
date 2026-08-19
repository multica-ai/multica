package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	create := httptest.NewRecorder()
	testHandler.CreateAgent(create, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":       fmt.Sprintf("starter-prompts-%d", time.Now().UnixNano()),
		"runtime_id": handlerTestRuntimeID(t),
		"starter_prompts": []map[string]string{{
			"label":  "  Review a PR  ",
			"prompt": "  Review the most relevant open pull request.  ",
		}},
	}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", create.Code, create.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})
	if len(created.StarterPrompts) != 1 ||
		created.StarterPrompts[0].Label != "Review a PR" ||
		created.StarterPrompts[0].Prompt != "Review the most relevant open pull request." {
		t.Fatalf("created starter_prompts = %#v", created.StarterPrompts)
	}

	omitted := httptest.NewRecorder()
	testHandler.UpdateAgent(omitted, withURLParam(
		newRequest(http.MethodPut, "/api/agents/"+created.ID, map[string]any{
			"description": "starter prompts unchanged",
		}),
		"id",
		created.ID,
	))
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted update status = %d, want 200: %s", omitted.Code, omitted.Body.String())
	}
	var preserved AgentResponse
	if err := json.NewDecoder(omitted.Body).Decode(&preserved); err != nil {
		t.Fatalf("decode omitted update response: %v", err)
	}
	if len(preserved.StarterPrompts) != 1 {
		t.Fatalf("omitted update starter_prompts = %#v, want preserved prompt", preserved.StarterPrompts)
	}

	cleared := httptest.NewRecorder()
	testHandler.UpdateAgent(cleared, withURLParam(
		newRequest(http.MethodPut, "/api/agents/"+created.ID, map[string]any{
			"starter_prompts": []AgentStarterPrompt{},
		}),
		"id",
		created.ID,
	))
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200: %s", cleared.Code, cleared.Body.String())
	}
	var clearedAgent AgentResponse
	if err := json.NewDecoder(cleared.Body).Decode(&clearedAgent); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if len(clearedAgent.StarterPrompts) != 0 {
		t.Fatalf("cleared starter_prompts = %#v, want empty", clearedAgent.StarterPrompts)
	}
}

package turn_test

import (
	"encoding/json"
	"strings"
	"testing"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	chturn "github.com/multica-ai/multica/server/internal/channel/turn"
)

func TestBuildPrompt_IncludesContextEntities(t *testing.T) {
	t.Parallel()

	req := chturn.Request{
		WorkspaceID: "ws-1",
		Text:        "把它关掉",
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-68", EntityType: channelconversation.EntityTypeIssue},
		},
	}

	prompt := chturn.BuildPrompt(req)
	if !strings.Contains(prompt, "Conversation context:") {
		t.Fatal("prompt missing 'Conversation context:' section")
	}
	if !strings.Contains(prompt, "STA-68") {
		t.Fatal("prompt missing context entity STA-68")
	}
}

func TestBuildPrompt_IncludesPendingAction(t *testing.T) {
	t.Parallel()

	req := chturn.Request{
		WorkspaceID: "ws-1",
		Text:        "sta-82",
		PendingAction: &chturn.PendingAction{
			Kind:       "SetStatus",
			Params:     map[string]string{"status": "cancelled"},
			Missing:    []string{"issue_key"},
			Candidates: []string{"STA-82"},
			Question:   "Which issue should I cancel?",
		},
	}
	prompt := chturn.BuildPrompt(req)
	if !strings.Contains(prompt, "PendingAction from previous turn:") {
		t.Fatal("prompt missing pending action section")
	}
	if !strings.Contains(prompt, "execute the pending action") {
		t.Fatal("prompt missing issue-key-only pending action rule")
	}
	if !strings.Contains(prompt, "STA-82") || !strings.Contains(prompt, "cancelled") {
		t.Fatal("prompt missing pending action details")
	}
}

func TestParseAgentOutput_StripsStateBlock(t *testing.T) {
	t.Parallel()

	raw := "你想关掉哪个 Issue？\n<multica_channel_state>\n" +
		`{"pending_action":{"kind":"SetStatus","params":{"status":"cancelled"},"missing":["issue_key"],"candidates":["sta-82"]}}` +
		"\n</multica_channel_state>"
	result, err := chturn.ParseAgentOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "你想关掉哪个 Issue？" {
		t.Fatalf("reply = %q", result.Reply)
	}
	if result.State.PendingAction == nil || result.State.PendingAction.Candidates[0] != "STA-82" {
		t.Fatalf("pending action = %+v", result.State.PendingAction)
	}
	payload, ok := chturn.MarshalStatePayload(result.State)
	if !ok {
		t.Fatal("state payload should marshal")
	}
	var decoded chturn.StatePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("state payload invalid json: %v", err)
	}
}

func TestBuildPrompt_IncludesExplicitEntities(t *testing.T) {
	t.Parallel()

	req := chturn.Request{
		WorkspaceID: "ws-1",
		Text:        "看看这个",
		ExplicitEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-99", EntityType: channelconversation.EntityTypeIssue},
		},
	}

	prompt := chturn.BuildPrompt(req)
	if !strings.Contains(prompt, "Explicit context:") {
		t.Fatal("prompt missing explicit context section")
	}
	if !strings.Contains(prompt, "STA-99") {
		t.Fatal("prompt missing explicit entity STA-99")
	}
}

func TestBuildPrompt_IncludesQuotedText(t *testing.T) {
	t.Parallel()

	req := chturn.Request{
		WorkspaceID:     "ws-1",
		Text:            "详情看看",
		QuotedText:      "STA-68 的状态是 open",
		QuotedMessageID: "om_quoted_001",
	}

	prompt := chturn.BuildPrompt(req)
	if !strings.Contains(prompt, "The user quoted this message:") {
		t.Fatal("prompt missing quoted text section")
	}
}

func TestBuildPrompt_ContextResolutionRules(t *testing.T) {
	t.Parallel()

	req := chturn.Request{WorkspaceID: "ws-1", Text: "test"}
	prompt := chturn.BuildPrompt(req)
	if strings.Contains(prompt, "require an explicit issue key in the message") {
		t.Fatal("prompt still contains old strict rule")
	}
	if !strings.Contains(prompt, "resolvable from ExplicitEntities/ContextEntities") {
		t.Fatal("prompt missing new context-resolution rule")
	}
}

func TestBuildPrompt_CloseIssueRuleIsLanguageNeutral(t *testing.T) {
	t.Parallel()

	req := chturn.Request{WorkspaceID: "ws-1", Text: "close it"}
	prompt := chturn.BuildPrompt(req)
	if !strings.Contains(prompt, "in any language") {
		t.Fatal("prompt should describe close/cancel semantics as language-neutral")
	}
	if !strings.Contains(prompt, "status update to `cancelled`") {
		t.Fatal("prompt missing issue cancelled status rule")
	}
	if !strings.Contains(prompt, "confirmation code") {
		t.Fatal("prompt should distinguish issue cancellation from action-code cancellation")
	}
}

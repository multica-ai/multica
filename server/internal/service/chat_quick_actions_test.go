package service

import (
	"strings"
	"testing"
)

func TestSplitChatQuickActions(t *testing.T) {
	t.Parallel()
	input := "Answer first.\r\n\r\n```quick-actions\r\n" +
		`[{"label":" Draft a brief ","prompt":"Write the full brief","primary":true},` +
		`{"label":"Plan next steps","prompt":"","primary":true},` +
		`{"label":"Draft a brief","prompt":"duplicate"},` +
		`{"label":"Define success","prompt":"Define the success metric"}]` +
		"\r\n```\r\n"

	visible, actions := splitChatQuickActions(input)
	if visible != "Answer first." {
		t.Fatalf("visible reply = %q", visible)
	}
	if len(actions) != 3 {
		t.Fatalf("actions len = %d, want 3: %+v", len(actions), actions)
	}
	if actions[0].Label != "Draft a brief" || actions[0].Prompt != "Write the full brief" || !actions[0].Primary {
		t.Fatalf("first action was not normalized: %+v", actions[0])
	}
	if actions[1].Prompt != "Plan next steps" || actions[1].Primary {
		t.Fatalf("second action must default its prompt and lose duplicate primary: %+v", actions[1])
	}
	if actions[2].Label != "Define success" {
		t.Fatalf("third action = %+v", actions[2])
	}
}

func TestSplitChatQuickActionsPromotesFirstAndSupportsActionsOnly(t *testing.T) {
	t.Parallel()
	visible, actions := splitChatQuickActions("```quick-actions\n[{\"label\":\"Continue\",\"prompt\":\"Continue the work\"}]\n```")
	if visible != "" {
		t.Fatalf("actions-only visible body = %q, want empty", visible)
	}
	if len(actions) != 1 || !actions[0].Primary {
		t.Fatalf("first action should become primary: %+v", actions)
	}
}

func TestSplitChatQuickActionsStripsMalformedTrailingProtocol(t *testing.T) {
	t.Parallel()
	visible, actions := splitChatQuickActions("Useful answer.\n```quick-actions\nnot json\n```")
	if visible != "Useful answer." || len(actions) != 0 {
		t.Fatalf("malformed footer should be stripped safely: visible=%q actions=%+v", visible, actions)
	}
}

func TestSplitChatQuickActionsLeavesMidResponseFenceVisible(t *testing.T) {
	t.Parallel()
	input := "Example:\n```quick-actions\n[]\n```\nMore explanation."
	visible, actions := splitChatQuickActions(input)
	if visible != input || actions != nil {
		t.Fatalf("non-trailing fence changed: visible=%q actions=%+v", visible, actions)
	}
}

func TestSplitChatQuickActionsLeavesMidResponseFenceWhenLaterFenceEndsReply(t *testing.T) {
	t.Parallel()
	input := "Here is the quick-actions format:\n" +
		"```quick-actions\n" +
		`[{"label":"Go","prompt":"Go now"}]` +
		"\n```\n" +
		"And here is a bash example:\n" +
		"```bash\nls -la\n```"
	visible, actions := splitChatQuickActions(input)
	if visible != input || actions != nil {
		t.Fatalf("mid-response quick-actions example changed: visible=%q actions=%+v", visible, actions)
	}
}

func TestSplitChatQuickActionsTruncatesByRune(t *testing.T) {
	t.Parallel()
	label := strings.Repeat("深", chatQuickActionLabelMax+5)
	input := "Done.\n```quick-actions\n[{\"label\":\"" + label + "\",\"prompt\":\"go\"}]\n```"
	_, actions := splitChatQuickActions(input)
	if got := len([]rune(actions[0].Label)); got != chatQuickActionLabelMax {
		t.Fatalf("label rune length = %d, want %d", got, chatQuickActionLabelMax)
	}
}

func TestParseChatQuickActionsOutputBareArray(t *testing.T) {
	t.Parallel()
	actions := parseChatQuickActionsOutput(
		`[{"label":"Draft it","prompt":"Draft the plan","primary":true},{"label":"List risks","prompt":"List the risks"}]`)
	if len(actions) != 2 || actions[0].Label != "Draft it" || !actions[0].Primary || actions[1].Primary {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestParseChatQuickActionsOutputToleratesFencesAndProse(t *testing.T) {
	t.Parallel()
	wrapped := "Sure, here are the suggestions:\n```json\n" +
		`[{"label":"Continue","prompt":"Continue the work"}]` +
		"\n```\nLet me know if you need more."
	actions := parseChatQuickActionsOutput(wrapped)
	if len(actions) != 1 || actions[0].Label != "Continue" || !actions[0].Primary {
		t.Fatalf("fence-wrapped output must still parse and promote primary: %+v", actions)
	}
}

func TestParseChatQuickActionsOutputDegradesToNone(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"empty":         "",
		"empty array":   "[]",
		"no array":      "I have no suggestions this time.",
		"broken json":   `[{"label":"x","prompt":`,
		"non-array":     `{"label":"x","prompt":"y"}`,
		"blank entries": `[{"label":"  ","prompt":"y"}]`,
	} {
		if actions := parseChatQuickActionsOutput(raw); len(actions) != 0 {
			t.Fatalf("%s: expected no actions, got %+v", name, actions)
		}
	}
}

func TestParseChatQuickActionsOutputAppliesSanitizeContract(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("剑", 700)
	actions := parseChatQuickActionsOutput(
		`[{"label":"A","prompt":"` + long + `"},{"label":"a","prompt":"dup"},{"label":"B"},{"label":"C","prompt":"c"},{"label":"D","prompt":"d"}]`)
	if len(actions) != 3 {
		t.Fatalf("cap at three after dedup, got %+v", actions)
	}
	if got := len([]rune(actions[0].Prompt)); got != 500 {
		t.Fatalf("prompt must truncate to 500 runes, got %d", got)
	}
	if actions[1].Prompt != "B" {
		t.Fatalf("empty prompt must default to label, got %+v", actions[1])
	}
	if !actions[0].Primary {
		t.Fatal("first action must be promoted primary when none marked")
	}
}

func TestParseChatQuickActionsOutputHandlesBracketsInProse(t *testing.T) {
	t.Parallel()
	wrapped := "Here's [my] take on it:\n```json\n" +
		`[{"label":"Ship it","prompt":"Ship the fix"}]` +
		"\n```"
	actions := parseChatQuickActionsOutput(wrapped)
	if len(actions) != 1 || actions[0].Label != "Ship it" {
		t.Fatalf("stray prose brackets must not break fenced parsing: %+v", actions)
	}
}

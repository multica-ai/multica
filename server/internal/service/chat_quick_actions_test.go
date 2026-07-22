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

func TestSplitChatQuickActionsTruncatesByRune(t *testing.T) {
	t.Parallel()
	label := strings.Repeat("深", chatQuickActionLabelMax+5)
	input := "Done.\n```quick-actions\n[{\"label\":\"" + label + "\",\"prompt\":\"go\"}]\n```"
	_, actions := splitChatQuickActions(input)
	if got := len([]rune(actions[0].Label)); got != chatQuickActionLabelMax {
		t.Fatalf("label rune length = %d, want %d", got, chatQuickActionLabelMax)
	}
}

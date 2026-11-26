package agent

import (
	"slices"
	"testing"
)

func TestWithClaudeTodoToolsAddsSwitch(t *testing.T) {
	got := withClaudeTodoTools([]string{"PATH=/bin"})
	if !slices.Contains(got, "CLAUDE_CODE_ENABLE_TODO_TOOLS=1") {
		t.Fatalf("expected todo tools switch, got %v", got)
	}
}

func TestWithClaudeTodoToolsKeepsExplicitValue(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDE_CODE_ENABLE_TODO_TOOLS=0"}
	got := withClaudeTodoTools(env)
	if len(got) != 2 || got[1] != "CLAUDE_CODE_ENABLE_TODO_TOOLS=0" {
		t.Fatalf("explicit value must win, got %v", got)
	}
}

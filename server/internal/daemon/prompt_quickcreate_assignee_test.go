package daemon

import (
	"strings"
	"testing"
)

// TestQuickCreateDefaultsToSelfWhenNoPeers covers the single-agent
// workspace case: with no peers in the task context, the prompt MUST
// instruct the picker to self-assign by default. This preserves today's
// behavior for installs that haven't added a second agent.
func TestQuickCreateDefaultsToSelfWhenNoPeers(t *testing.T) {
	t.Parallel()
	task := Task{
		QuickCreatePrompt: "fix the dashboard graph height",
		Agent:             &AgentData{Name: "Hermes"},
		// PeerAgents intentionally empty.
	}
	out := BuildPrompt(task)

	// Self-assign default present; routing language NOT present.
	if !strings.Contains(out, `default to YOURSELF: pass `+"`"+`--assignee "Hermes"`+"`") {
		t.Errorf("expected single-agent self-assign instruction; got:\n%s", out)
	}
	if strings.Contains(out, "decide based on the work") {
		t.Errorf("did not expect peer-routing language in single-agent workspace; got:\n%s", out)
	}
}

// TestQuickCreateAllowsPeerRouteWhenPeersExist is the regression bug:
// before this fix the prompt FORCED self-assign even when the workspace
// had a coding-agent peer the picker should route to. With peers present,
// the prompt now lets the picker's persona decide.
func TestQuickCreateAllowsPeerRouteWhenPeersExist(t *testing.T) {
	t.Parallel()
	task := Task{
		QuickCreatePrompt: "fix the dashboard graph height",
		Agent:             &AgentData{Name: "Hermes"},
		PeerAgents: []PeerAgentData{
			{ID: "p-claude", Name: "Claude Code", Instructions: "Coding agent"},
		},
	}
	out := BuildPrompt(task)

	// New language: route OR self-assign, picker decides.
	if !strings.Contains(out, "decide based on the work") {
		t.Errorf("expected peer-aware routing language; got:\n%s", out)
	}
	// Self-assign fallback STILL referenced (with the picker's name) so
	// coding-agent pickers don't lose the default.
	if !strings.Contains(out, `--assignee "Hermes"`) {
		t.Errorf("expected self-assign fallback with picker name; got:\n%s", out)
	}
	// "Pick exactly one assignee" guard against the LLM passing two names.
	if !strings.Contains(out, "exactly one assignee") {
		t.Errorf("expected exactly-one-assignee guard; got:\n%s", out)
	}
	// The forcing language from the old prompt MUST be gone — that was
	// the regression that made Hermes self-assign coding work.
	if strings.Contains(out, "The picker agent is the expected owner") {
		t.Errorf("old forcing language still present; got:\n%s", out)
	}
}

// TestQuickCreateNoAgentNameFallback covers a defensive third path:
// when task.Agent is nil (legacy / malformed task), the prompt falls
// back to a generic "default to YOURSELF" instruction rather than
// emitting an empty `--assignee ""` invocation.
func TestQuickCreateNoAgentNameFallback(t *testing.T) {
	t.Parallel()
	task := Task{
		QuickCreatePrompt: "fix the dashboard graph height",
		// Agent: nil
	}
	out := BuildPrompt(task)
	if !strings.Contains(out, "default to YOURSELF (the picker agent)") {
		t.Errorf("expected generic self-assign fallback; got:\n%s", out)
	}
}

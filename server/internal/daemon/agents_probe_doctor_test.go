package daemon

import "testing"

func TestHasDiscoveredToolRequiresExecutable(t *testing.T) {
	set := stubAgentProbe(t, map[string]AgentEntry{
		"museapp": {Command: "museapp"},
	})
	for _, id := range []string{"", "museapp", "hermes"} {
		if HasDiscoveredTool(id) {
			t.Errorf("endpoint-only discovery accepted as executable for %q", id)
		}
	}
	set(map[string]AgentEntry{
		"museapp": {Command: "museapp"},
		"hermes":  {Command: "hermes", Path: "/fixture/hermes"},
	})
	for _, id := range []string{"", "hermes"} {
		if !HasDiscoveredTool(id) {
			t.Errorf("discovered executable rejected for %q", id)
		}
	}
	if HasDiscoveredTool("museapp") {
		t.Fatal("endpoint runtime accepted alongside an executable")
	}
}

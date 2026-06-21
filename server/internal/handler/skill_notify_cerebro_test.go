package handler

import "testing"

// FIR-1587 — the inbox body must always say WHO did it, WHAT changed, and WHY,
// in plain language with real names (no short UUIDs).

func TestSkillChangeRequestBody_WhoWhatWhyAndSession(t *testing.T) {
	got := skillChangeRequestBody("Jesper Hvejsel", "ab-test-setup", "1.0.0", "1.1.0",
		"Clarify the rollout step", true)
	want := "Jesper Hvejsel proposed an update to ab-test-setup (v1.0.0 → v1.1.0). " +
		"Why: Clarify the rollout step · Proposed in a recorded work session — open the proposal for the trail."
	if got != want {
		t.Fatalf("body mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestSkillChangeRequestBody_MissingReasonIsFlagged(t *testing.T) {
	got := skillChangeRequestBody("Mia", "deploy", "2.0.0", "2.1.0", "   ", false)
	want := "Mia proposed an update to deploy (v2.0.0 → v2.1.0). " +
		"No reason given yet — ask the proposer to explain why."
	if got != want {
		t.Fatalf("body mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestSkillForkedAndAgentAssignedBodies_NameTheActor(t *testing.T) {
	if got, want := skillForkedBody("Filip Evaldsen", "page-cro", "page-cro-v2"),
		"Filip Evaldsen forked page-cro into a new skill, page-cro-v2"; got != want {
		t.Fatalf("fork body mismatch:\n got: %q\nwant: %q", got, want)
	}
	if got, want := skillAgentAssignedBody("Jesper Hvejsel", "deploy", "Sara - CTO"),
		"Jesper Hvejsel assigned deploy to the agent Sara - CTO"; got != want {
		t.Fatalf("assign body mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// The claim handler stores what AgentConfigDigest returns and compares it on
// the next run, so both properties tested here — stability across calls and
// sensitivity to the delivered instructions — are load-bearing (MUL-7082).
package service

import (
	"strings"
	"testing"
)

func TestAgentConfigDigest_StableForSameInstructions(t *testing.T) {
	first := AgentConfigDigest("review PRs the careful way")
	second := AgentConfigDigest("review PRs the careful way")
	if first != second {
		t.Fatalf("digest is not stable: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, agentConfigDigestVersion+":") {
		t.Fatalf("digest must carry its version so a later formula never compares equal: %q", first)
	}
}

func TestAgentConfigDigest_ChangesWithInstructions(t *testing.T) {
	if AgentConfigDigest("RULE A") == AgentConfigDigest("RULE B") {
		t.Fatal("different instructions must digest differently")
	}
	// Whitespace is part of the instruction text the model reads, so a
	// reformat is a change. This is the conservative direction: an extra cold
	// start, never a stale resume.
	if AgentConfigDigest("RULE A") == AgentConfigDigest("RULE A\n") {
		t.Fatal("instruction text must be digested verbatim")
	}
	// An agent with no instructions still gets a stable value rather than an
	// empty one, so the gate does not read "unknown" on every run.
	if AgentConfigDigest("") == "" {
		t.Fatal("empty instructions must still produce a digest")
	}
}

func TestAgentConfigChanged(t *testing.T) {
	a := AgentConfigDigest("RULE A")
	b := AgentConfigDigest("RULE B")

	if AgentConfigChanged(a, a) {
		t.Fatal("an unchanged configuration must resume")
	}
	if !AgentConfigChanged(a, b) {
		t.Fatal("a changed configuration must not resume")
	}
	// Rows written before the column existed, and claims whose digest write
	// lost its CAS, carry nothing. Unknown is not stale: they keep resuming so
	// shipping the gate does not cold-start every live conversation at once.
	if AgentConfigChanged("", b) {
		t.Fatal("a prior task with no recorded digest must still resume")
	}
	if AgentConfigChanged(a, "") {
		t.Fatal("a claim that could not compute a digest must not drop the session")
	}
}

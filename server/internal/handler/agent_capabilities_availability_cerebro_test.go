package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
)

// fakeEvidence is a ledger stand-in keyed by canonical capability ID. Only the
// entries it is given exist; everything else is unprobed, exactly like a real
// ledger.
type fakeEvidence map[string]availabilityevidence.Evidence

func (f fakeEvidence) Lookup(capabilityID string, rt availabilityevidence.RuntimeType) availabilityevidence.Evidence {
	if e, ok := f[capabilityID]; ok && e.RuntimeType == rt {
		return e
	}
	return availabilityevidence.Evidence{
		CapabilityID: capabilityID,
		RuntimeType:  rt,
		Level:        availabilityevidence.LevelDeclared,
		Reason:       "no probe has run for this capability on this runtime",
	}
}

func builtinTool(key string) AgentCapabilityTool {
	return AgentCapabilityTool{Key: key, Source: capSourceBuiltin, Permission: "allow"}
}

// The requirement this whole step exists for: a tool a probe PROVED is the only
// thing the card may present as reality.
func TestAvailabilityMarksProvenToolVerified(t *testing.T) {
	evidence := fakeEvidence{
		"platform:get_agent_capabilities": {
			CapabilityID: "platform:get_agent_capabilities",
			RuntimeType:  availabilityevidence.RuntimeFirtalGateway,
			Level:        availabilityevidence.LevelVerified,
			Reason:       "a test call proved both access and refusal",
		},
	}

	tools, summary := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{builtinTool("get_agent_capabilities")},
		availabilityevidence.RuntimeFirtalGateway, evidence)

	got := tools[0].Availability
	if !got.Proven {
		t.Errorf("a verified tool must be presented as proven, got %+v", got)
	}
	if got.Level != string(availabilityevidence.LevelVerified) {
		t.Errorf("level = %q, want %q", got.Level, availabilityevidence.LevelVerified)
	}
	if summary.Verified != 1 || summary.Unproven != 0 {
		t.Errorf("summary = %+v, want 1 verified / 0 unproven", summary)
	}
	if summary.Status != capStatusKnown {
		t.Errorf("status = %q, want %q", summary.Status, capStatusKnown)
	}
}

// A granted tool nobody probed is the Kristian case: configuration says yes and
// the runtime may have nothing to call. It must read as NOT proven, with the
// reason visible — never as a capability the agent has.
func TestAvailabilityMarksUnprobedToolNotProven(t *testing.T) {
	tools, summary := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{builtinTool("get_agent_capabilities")},
		availabilityevidence.RuntimeFirtalGateway, fakeEvidence{})

	got := tools[0].Availability
	if got.Proven {
		t.Error("an unprobed tool must never be presented as proven")
	}
	if got.Level != string(availabilityevidence.LevelDeclared) {
		t.Errorf("level = %q, want %q", got.Level, availabilityevidence.LevelDeclared)
	}
	if got.Reason == "" {
		t.Error("an unproven tool must say why it is unproven")
	}
	if summary.Verified != 0 || summary.Unproven != 1 {
		t.Errorf("summary = %+v, want 0 verified / 1 unproven", summary)
	}
	if tools[0].Permission != "deny" {
		t.Errorf("permission = %q, want deny when the access engine rejects missing runtime evidence", tools[0].Permission)
	}
}

// discovered means the capability was found but the gate was never proved. Found
// is not the same as working, so it is not reality either.
func TestAvailabilityMarksDiscoveredToolNotProven(t *testing.T) {
	evidence := fakeEvidence{
		"platform:get_agent_capabilities": {
			CapabilityID: "platform:get_agent_capabilities",
			RuntimeType:  availabilityevidence.RuntimeLocal,
			Level:        availabilityevidence.LevelDiscovered,
			Reason:       "found on the runtime's surface but no test call was made",
		},
	}

	tools, _ := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{builtinTool("get_agent_capabilities")},
		availabilityevidence.RuntimeLocal, evidence)

	if tools[0].Availability.Proven {
		t.Error("discovered is not proof: the gate was never tested")
	}
	if tools[0].Permission != "allow" {
		t.Errorf("permission = %q, want allow because discovered is present on the live surface", tools[0].Permission)
	}
}

// Evidence is per runtime. A tool proved on the Gateway says nothing about an
// agent running elsewhere — that cross-runtime leak is exactly the drift this
// model exists to stop.
func TestAvailabilityNeverBorrowsAnotherRuntimesProof(t *testing.T) {
	evidence := fakeEvidence{
		"platform:get_agent_capabilities": {
			CapabilityID: "platform:get_agent_capabilities",
			RuntimeType:  availabilityevidence.RuntimeFirtalGateway,
			Level:        availabilityevidence.LevelVerified,
			Reason:       "a test call proved both access and refusal",
		},
	}

	tools, _ := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{builtinTool("get_agent_capabilities")},
		availabilityevidence.RuntimeLocal, evidence)

	if tools[0].Availability.Proven {
		t.Error("a Gateway proof must not make the tool proven on a local runtime")
	}
}

// With no evidence source wired at all, the card must say it does not know —
// not report a confident "nothing is proven" it cannot back up.
func TestAvailabilityReportsUnknownWithoutEvidenceSource(t *testing.T) {
	tools, summary := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{builtinTool("get_agent_capabilities")},
		availabilityevidence.RuntimeFirtalGateway, nil)

	if summary.Status != capStatusUnknown {
		t.Errorf("status = %q, want %q", summary.Status, capStatusUnknown)
	}
	if tools[0].Availability.Proven {
		t.Error("no evidence source can never yield a proven tool")
	}
}

// A row with no canonical name cannot be keyed to a probe. Say so plainly
// instead of implying the probe ran and found nothing.
func TestAvailabilityExplainsToolWithNoCanonicalName(t *testing.T) {
	tools, _ := applyAgentCapabilityAvailability(
		[]AgentCapabilityTool{{Key: "some_scanned_tool", Source: capSourceScan, Permission: "allow"}},
		availabilityevidence.RuntimeFirtalGateway, fakeEvidence{})

	got := tools[0].Availability
	if got.Proven {
		t.Error("an unmappable tool must not be proven")
	}
	if got.Reason != capAvailabilityNoCanonicalName {
		t.Errorf("reason = %q, want %q", got.Reason, capAvailabilityNoCanonicalName)
	}
	if tools[0].Permission != "allow" {
		t.Errorf("permission = %q, want policy answer preserved outside the canonical engine", tools[0].Permission)
	}
}

func TestAvailabilitySummaryNamesTheRuntimeItJudged(t *testing.T) {
	_, summary := applyAgentCapabilityAvailability(nil, availabilityevidence.RuntimeClaudeCode, fakeEvidence{})
	if summary.RuntimeType != string(availabilityevidence.RuntimeClaudeCode) {
		t.Errorf("runtime_type = %q, want %q", summary.RuntimeType, availabilityevidence.RuntimeClaudeCode)
	}
}

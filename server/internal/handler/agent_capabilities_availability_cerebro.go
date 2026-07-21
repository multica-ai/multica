package handler

// CEREBRO-PATCH(agent-capabilities-availability): FIR-3398 Trin 2 — present the
// availability evidence on the capabilities card, so a reader can tell what an
// agent PROVABLY has from what configuration merely claims.
//
// Every other section of this card answers "what is this agent granted?". That
// question has no truth value on its own: `get_agent_capabilities` was granted
// to Kristian's agent while the Firtal Gateway had no implementation to call —
// the grant said yes and the runtime had nothing. This section answers the
// different question "what has been PROVED on the runtime this agent actually
// runs on?", and only availabilityevidence.LevelVerified counts as an answer.
//
// Permission is the final canonical answer: policy Allow is reduced to Deny
// when the shared access engine says the live runtime evidence is insufficient.
// Availability keeps the evidence and explanation visible alongside it.

import (
	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
)

// capSourceBuiltin is the tool-policy source stamped on platform built-in tools
// (toolpolicy.registryToolSource). These are the rows that carry a canonical
// platform capability name, and therefore the rows a probe can be keyed to.
const capSourceBuiltin = "builtin"

// capAvailabilityNoCanonicalName is the reason given for a tool that Trin 1's
// catalog does not mint a canonical platform name for (a scanned runtime-native
// tool, a connection tool). No probe can be keyed to it, so it is unproven —
// but for a different reason than a probe that ran and found nothing, and the
// card must not blur the two.
const capAvailabilityNoCanonicalName = "this tool has no canonical platform capability name, so no probe is keyed to it"

// capAvailabilityNoSource is the reason given when no evidence source is wired
// at all. "We have not looked" is not "we looked and found nothing".
const capAvailabilityNoSource = "no availability probe is wired on this server, so nothing has been proved"

// AgentCapabilityEvidenceLookup reads what has been PROVED about one capability
// on one runtime. Satisfied by *availabilityevidence.Ledger.
//
// It is a read seam, not a prober: the card reports evidence, it never produces
// it. A nil lookup means no probe is wired, which the card reports as unknown
// rather than as an absence of capabilities.
type AgentCapabilityEvidenceLookup interface {
	Lookup(capabilityID string, rt availabilityevidence.RuntimeType) availabilityevidence.Evidence
}

// AgentCapabilityAvailability is the evidence behind one tool on the runtime the
// agent actually runs on. Proven is the field a reader should trust: it is true
// only for verified, where a test call proved both that the gate lets the right
// caller in and turns the wrong caller away. Reason is always populated, so
// "not proven" never has to be interpreted.
type AgentCapabilityAvailability struct {
	Level  string `json:"level"` // declared | discovered | verified
	Proven bool   `json:"proven"`
	Reason string `json:"reason"`
}

// AgentCapabilityAvailabilitySummary is the card-level headline: which runtime
// the evidence was read for, whether we have any evidence at all, and how the
// agent's tools split between proved and unproven.
type AgentCapabilityAvailabilitySummary struct {
	RuntimeType string `json:"runtime_type"`
	Status      string `json:"status"` // known | unknown
	Verified    int    `json:"verified"`
	Unproven    int    `json:"unproven"`
}

// applyAgentCapabilityAvailability stamps each tool with its evidence and
// returns the card-level summary. Pure: it decides nothing and reads nothing but
// the lookup it is handed.
func applyAgentCapabilityAvailability(
	tools []AgentCapabilityTool,
	rt availabilityevidence.RuntimeType,
	lookup AgentCapabilityEvidenceLookup,
) ([]AgentCapabilityTool, AgentCapabilityAvailabilitySummary) {
	summary := AgentCapabilityAvailabilitySummary{RuntimeType: string(rt), Status: capStatusKnown}
	if lookup == nil {
		summary.Status = capStatusUnknown
	}

	for i, tool := range tools {
		availability := toolAvailability(tool, rt, lookup)
		tools[i].Availability = availability
		if tool.Permission == string(accessdecision.PolicyAllow) {
			capabilityID, canonical := canonicalCapabilityID(tool)
			if canonical {
				result := accessdecision.Evaluate(accessdecision.EvaluationInput{
					CanonicalCapabilityID: capabilityID,
					PolicyDecision:        accessdecision.PolicyAllow,
					EvidenceLevel:         availabilityevidence.Level(availability.Level),
				})
				if result.Decision == accessdecision.DecisionDeny {
					tools[i].Permission = string(accessdecision.DecisionDeny)
					tools[i].Reason = result.Reason
				}
			}
		}
		if availability.Proven {
			summary.Verified++
		} else {
			summary.Unproven++
		}
	}
	return tools, summary
}

// toolAvailability resolves one tool's evidence. Every path that cannot reach a
// proof returns declared with the reason it could not — a tool is never proven
// by default, and never unproven without saying why.
func toolAvailability(
	tool AgentCapabilityTool,
	rt availabilityevidence.RuntimeType,
	lookup AgentCapabilityEvidenceLookup,
) AgentCapabilityAvailability {
	unproven := func(reason string) AgentCapabilityAvailability {
		return AgentCapabilityAvailability{Level: string(availabilityevidence.LevelDeclared), Reason: reason}
	}

	id, ok := canonicalCapabilityID(tool)
	if !ok {
		return unproven(capAvailabilityNoCanonicalName)
	}
	if lookup == nil {
		return unproven(capAvailabilityNoSource)
	}

	evidence := lookup.Lookup(id, rt)
	return AgentCapabilityAvailability{
		Level:  string(evidence.Level),
		Proven: evidence.Level.IsReality(),
		Reason: evidence.Reason,
	}
}

// canonicalCapabilityID mints the Trin 1 canonical capability name for a card
// row, which is the key every probe records evidence under. Going through
// capabilitycatalog rather than string-concatenating "platform:" here is what
// keeps the two steps from drifting apart on naming.
func canonicalCapabilityID(tool AgentCapabilityTool) (string, bool) {
	if tool.Source != capSourceBuiltin || tool.Key == "" {
		return "", false
	}
	return capabilitycatalog.PlatformTool(tool.Key).ID, true
}

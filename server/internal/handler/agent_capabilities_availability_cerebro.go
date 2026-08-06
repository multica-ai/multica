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
// Permission is the effective policy answer. Availability keeps runtime
// evidence separate, and callability combines policy, delivery availability,
// and enforcement without rewriting that policy history.

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

// capSourceBuiltin is the tool-policy source stamped on platform built-in tools
// (toolpolicy.registryToolSource). These are the rows that carry a canonical
// platform capability name, and therefore the rows a probe can be keyed to.
const capSourceBuiltin = "builtin"

const capSourceRuntimeReport = "runtime_report"

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
	Discovered  int    `json:"discovered"`
	Declared    int    `json:"declared"`
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
	return applyAgentCapabilityAvailabilityForProvider(tools, rt, "", lookup)
}

func applyAgentCapabilityAvailabilityForProvider(
	tools []AgentCapabilityTool,
	rt availabilityevidence.RuntimeType,
	provider string,
	lookup AgentCapabilityEvidenceLookup,
) ([]AgentCapabilityTool, AgentCapabilityAvailabilitySummary) {
	summary := AgentCapabilityAvailabilitySummary{RuntimeType: string(rt), Status: capStatusKnown}
	if lookup == nil {
		summary.Status = capStatusUnknown
	}

	for i, tool := range tools {
		availability := toolAvailabilityForProvider(tool, rt, provider, lookup)
		tools[i].Availability = availability
		tools[i].Verified = availability.Proven
		if tool.Source != platformcatalog.Source {
			level := availabilityevidence.Level(availability.Level)
			tools[i].Available = level == availabilityevidence.LevelDiscovered || level == availabilityevidence.LevelVerified
		}
		tools[i].Allowed = tools[i].Permission == "allow"
		if availability.Proven {
			summary.Verified++
		} else {
			summary.Unproven++
			if availabilityevidence.Level(availability.Level) == availabilityevidence.LevelDiscovered {
				summary.Discovered++
			} else {
				summary.Declared++
			}
		}
		tools[i].Callable = tools[i].Allowed && tools[i].Available && tools[i].Enforced
		setCapabilityBlockExplanation(&tools[i])
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
	return toolAvailabilityForProvider(tool, rt, "", lookup)
}

func toolAvailabilityForProvider(
	tool AgentCapabilityTool,
	rt availabilityevidence.RuntimeType,
	provider string,
	lookup AgentCapabilityEvidenceLookup,
) AgentCapabilityAvailability {
	unproven := func(reason string) AgentCapabilityAvailability {
		return AgentCapabilityAvailability{Level: string(availabilityevidence.LevelDeclared), Reason: reason}
	}

	id, canonical := canonicalCapabilityID(tool, provider)
	if canonical && lookup != nil {
		evidence := lookup.Lookup(id, rt)
		if evidence.Level == availabilityevidence.LevelVerified {
			return AgentCapabilityAvailability{
				Level:  string(evidence.Level),
				Proven: true,
				Reason: evidence.Reason,
			}
		}
	}

	// A runtime_report row is the runtime's current inventory snapshot and a
	// scan row came from a live tools/list surface. Both are direct discovery
	// evidence. They do not earn Verified until a two-sided call probe exists.
	if tool.Source == capSourceRuntimeReport || tool.Source == capSourceScan {
		return AgentCapabilityAvailability{
			Level:  string(availabilityevidence.LevelDiscovered),
			Reason: "found in the agent runtime's current tool inventory",
		}
	}
	if !canonical {
		return unproven(capAvailabilityNoCanonicalName)
	}
	if lookup == nil {
		return unproven(capAvailabilityNoSource)
	}
	evidence := lookup.Lookup(id, rt)
	return AgentCapabilityAvailability{Level: string(evidence.Level), Proven: evidence.Level.IsReality(), Reason: evidence.Reason}
}

// canonicalCapabilityID mints the Trin 1 canonical capability name for a card
// row, which is the key every probe records evidence under. Going through
// capabilitycatalog rather than string-concatenating "platform:" here is what
// keeps the two steps from drifting apart on naming.
func canonicalCapabilityID(tool AgentCapabilityTool, provider string) (string, bool) {
	if tool.Key == "" {
		return "", false
	}
	switch tool.Source {
	case capSourceBuiltin, platformcatalog.Source:
		return capabilitycatalog.PlatformTool(tool.Key).ID, true
	case capSourceRuntimeReport:
		if provider == "" {
			return "", false
		}
		name := strings.TrimPrefix(tool.Key, "tools:")
		name = capabilitycatalog.CanonicalRuntimeToolName(provider, name)
		return capabilitycatalog.RuntimeNativeTool(provider, name, "tools:"+name).ID, true
	case capSourceScan:
		if _, ok := platformcatalog.ByKey(tool.Key); ok {
			return capabilitycatalog.PlatformTool(tool.Key).ID, true
		}
		if tool.Category == "multica" {
			return capabilitycatalog.BridgeTool(tool.Key).ID, true
		}
		if tool.Category != "" {
			name := tool.Title
			if name == "" {
				name = tool.Key
			}
			return capabilitycatalog.MCPConnectionTool(tool.Category, name).ID, true
		}
	}
	return "", false
}

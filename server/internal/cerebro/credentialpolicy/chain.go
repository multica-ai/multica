// Package credentialpolicy implements the per-credential permission chain for
// the cerebro control plane (FIR-1479 — "credentials as a permission type").
//
// Jesper's model: a "credential" is one Agent Vault box (e.g. 'bigquery',
// 'personal-jesper', a per-repo box). It is assigned to an actor — agent,
// person, or group — the same way a tool is assigned today via a tool grant.
// A ticked box = access to exactly that one vault box, nothing more. This is
// least-privilege: granting a box opens nothing else up, and a box no one has
// been granted is unreachable.
//
// This package is the resolver. It mirrors toolpolicy's five-layer chain so the
// permissions interface can render and resolve credentials through the same
// stacked layers it already uses for tools:
//
//	Workspace → Runtime → Agent → Group → User (or System)
//
// It differs from toolpolicy in two deliberate ways, because a credential is a
// GRANT (default-off, opt-in) rather than a capability ceiling (default-on,
// tighten-down):
//
//  1. There are only two concrete settings — Allow and Deny — never Ask. The
//     "are you sure" prompt belongs to the *action* taken with the underlying
//     secret (reveal / rotate), which cerebro_credential governs separately;
//     reachability of the box itself is a yes/no.
//
//  2. The base is Deny, and resolution is "nearest-to-the-ceiling explicit
//     layer wins" (an override fold), NOT toolpolicy's monotonic
//     most-restrictive fold. A monotonic fold cannot express a default-off
//     grant: with the base at Deny, a more-specific Allow could never raise it.
//     The grant UX Jesper asked for — workspace default off, tick this one
//     agent on — requires the agent layer's Allow to override the workspace
//     layer's (implicit) Deny. So a layer closer to the ceiling overrides the
//     layers below it; the user/system ceiling has the final say.
//
// Resolve is pure: settings in, decision out. It has no database dependency so
// it is exhaustively unit-testable.
package credentialpolicy

import "fmt"

// Setting is the per-layer choice for a single credential.
type Setting string

const (
	// SettingInherit defers to the layer below (toward the base). A layer that is
	// absent from the chain is treated as Inherit.
	SettingInherit Setting = "inherit"
	// SettingAllow grants reachability of the box at this layer.
	SettingAllow Setting = "allow"
	// SettingDeny revokes reachability of the box at this layer.
	SettingDeny Setting = "deny"
)

// Layer identifies one rung of the policy chain. The values match
// toolpolicy.Layer so the two chains share the permissions interface, the
// subject_id discriminator, and the database layer vocabulary.
type Layer string

const (
	// LayerWorkspace is the root layer — the workspace-wide default applied below
	// every runtime.
	LayerWorkspace Layer = "workspace"
	// LayerRuntime is the machine layer — the runtime's default for every agent.
	LayerRuntime Layer = "runtime"
	// LayerAgent is the per-agent override.
	LayerAgent Layer = "agent"
	// LayerGroup is the shared team policy.
	LayerGroup Layer = "group"
	// LayerUser is the ceiling — the requesting user's own access.
	LayerUser Layer = "user"
	// LayerSystem is the ceiling for runs with no human behind them (autopilot /
	// system-triggered). It is a PEER of LayerUser: exactly one of User or System
	// fills the ceiling slot per run, never both.
	LayerSystem Layer = "system"
)

// chainOrder lists layers from base (index 0) to ceiling (last). Resolution
// walks this order and the LAST explicit (non-Inherit) layer wins, so a layer
// closer to the ceiling overrides the layers below it. LayerUser and
// LayerSystem are mutually exclusive per run (one mandate actor), so listing
// both at the ceiling is safe: the absent one is Inherit and does not decide.
var chainOrder = []Layer{LayerWorkspace, LayerRuntime, LayerAgent, LayerGroup, LayerUser, LayerSystem}

// Input is the resolved per-layer chain for one credential and one
// (user/system, group(s), agent, runtime, workspace) context. A layer absent
// from Settings is treated as Inherit.
//
// Group membership is collapsed into a single LayerGroup setting before it
// reaches Resolve; use CombineGroups for the multi-group case.
type Input struct {
	// Settings holds the explicit choice at each layer. Missing == Inherit.
	Settings map[Layer]Setting
}

// Effective is the resolved verdict for one credential.
type Effective struct {
	// Setting is the combined result: always Allow or Deny (never Inherit).
	Setting Setting
	// DecidedBy is the layer that owns the verdict — the layer closest to the
	// ceiling with an explicit setting. Empty when no layer had an opinion and
	// the deny-by-default base decided.
	DecidedBy Layer
	// Reason is a short human-readable explanation suitable for audit rows and
	// tooltips, e.g. "Allowed by agent" or "Denied by default".
	Reason string
}

// Allowed reports whether the actor may reach this credential's box.
func (e Effective) Allowed() bool { return e.Setting == SettingAllow }

// Resolve folds the chain into a single Effective verdict.
//
// Algorithm (walk base → ceiling):
//
//  1. Start from Deny — least-privilege: a box no layer has granted is
//     unreachable.
//  2. An Inherit (or absent) setting passes the running value through.
//  3. A concrete setting (Allow or Deny) OVERRIDES the running value and takes
//     ownership of the verdict. Because the walk runs base → ceiling, a layer
//     closer to the ceiling overrides the layers below it, and the
//     user / system ceiling has the final say.
func Resolve(in Input) Effective {
	resolved := SettingDeny
	var decidedBy Layer

	for _, layer := range chainOrder {
		v := in.Settings[layer]
		switch v {
		case SettingAllow, SettingDeny:
			resolved = v
			decidedBy = layer
		default:
			// Inherit / absent / unknown — follow the layer below.
		}
	}

	return Effective{
		Setting:   resolved,
		DecidedBy: decidedBy,
		Reason:    reasonFor(resolved, decidedBy),
	}
}

// CombineGroups collapses the settings from every group an actor belongs to
// into the single value that enters Resolve at LayerGroup.
//
// Group membership is additive for a grant: belonging to one group that grants
// a box is enough to reach it, even if a sibling group is silent or denies. So
// an explicit Allow in any group wins over a Deny in another; an explicit Deny
// only takes effect when no group grants. Groups with no opinion (Inherit /
// absent) do not constrain. The user / system ceiling above can still override
// the combined group verdict.
func CombineGroups(settings ...Setting) Setting {
	combined := SettingInherit
	for _, s := range settings {
		switch s {
		case SettingAllow:
			return SettingAllow
		case SettingDeny:
			combined = SettingDeny
		default:
			// Inherit / absent — no opinion.
		}
	}
	return combined
}

func reasonFor(setting Setting, decidedBy Layer) string {
	if decidedBy == "" {
		return fmt.Sprintf("%s by default", labelFor(setting))
	}
	return fmt.Sprintf("%s by %s", labelFor(setting), decidedBy)
}

func labelFor(setting Setting) string {
	switch setting {
	case SettingAllow:
		return "Allowed"
	case SettingDeny:
		return "Denied"
	default:
		return string(setting)
	}
}

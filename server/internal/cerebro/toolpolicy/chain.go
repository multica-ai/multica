// Package toolpolicy implements the unified per-tool permission chain for the
// cerebro control plane (FIR-2230, phase 1 — "the engine").
//
// Before this package, three separate systems decided whether an agent could
// use a tool: the workspace-grant resolver (capability patterns), the per-tool
// runtime grant tables (boolean enabled + user/group access), and the group
// permission service (capability whitelists). Each computed its own answer and
// none expressed the product's actual model: every individual CLI tool and
// every individual MCP action is governed on its own, at five stacked layers,
// where each layer can only tighten the one below it.
//
// The layers, from base to ceiling, are:
//
//	Workspace → Runtime → Agent → Group → User
//
//	Workspace (ROOT)   The workspace-wide default for every runtime in it — the
//	                   root of the chain, authored under Settings (FIR-2284 Bid
//	                   5). Sets the starting point below the runtime layer.
//	Runtime            The machine's default for every agent on it. Tightens
//	                   under the workspace root.
//	Agent              Override on the individual agent. Can only tighten what
//	                   the runtime offers.
//	Group              Shared rules for a team. Tightens under the user ceiling.
//	User     (CEILING) The user's own access. Nothing below can be raised above
//	                   it — if the user denies, it is forbidden for everyone
//	                   below ("Capped by user").
//
// Each layer holds one Setting per tool: Allow, Ask, Deny, or Inherit. Inherit
// means "follow the layer below" (toward the base). The Effective setting the
// user sees is the whole chain combined: the most restrictive explicit setting
// across every layer, because a layer may only ever tighten access, never
// loosen it. This makes resolution a pure, order-independent fold — the layer
// ordering matters only for attribution (which layer to blame in the reason).
//
// Resolve is pure: settings in, decision out. It has no database dependency so
// it is exhaustively unit-testable.
package toolpolicy

import "fmt"

// Setting is the per-layer choice for a single tool.
type Setting string

const (
	// SettingInherit defers to the layer below (toward the base). A layer that
	// is absent from the chain is treated as Inherit.
	SettingInherit Setting = "inherit"
	// SettingAllow lets the tool run without asking. Least restrictive.
	SettingAllow Setting = "allow"
	// SettingAsk pauses the agent and routes an approval request to the inbox.
	SettingAsk Setting = "ask"
	// SettingDeny forbids the tool entirely. Most restrictive.
	SettingDeny Setting = "deny"
)

// Layer identifies one rung of the policy chain.
type Layer string

const (
	// LayerWorkspace is the root layer — the workspace-wide default applied below
	// every runtime. It is the lowest rung of the chain (FIR-2284 Bid 5).
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
	// system-triggered). It is a PEER of LayerUser at the mandate level: exactly
	// one of User or System fills the ceiling slot per run, never both. A System
	// is born from a User and is capped at that owner's permissions at authoring
	// time, so the human ceiling never disappears — it is inherited at birth
	// (FIR-1609).
	LayerSystem Layer = "system"
)

// chainOrder lists layers from base (index 0) to ceiling (last). Resolution
// walks this order so that, on ties, a higher layer takes responsibility for
// the effective value — which is what makes "Capped by user" attributable.
// LayerUser and LayerSystem are mutually exclusive per run (one mandate actor),
// so listing both at the ceiling is safe: the absent one is Inherit and does not
// constrain. Either can cap below the base, like a ceiling should.
var chainOrder = []Layer{LayerWorkspace, LayerRuntime, LayerAgent, LayerGroup, LayerUser, LayerSystem}

// rank maps a concrete setting to its restrictiveness. Higher is tighter.
// Inherit (and any unknown value) returns -1 — "no opinion, pass through".
func rank(s Setting) int {
	switch s {
	case SettingAllow:
		return 0
	case SettingAsk:
		return 1
	case SettingDeny:
		return 2
	default:
		return -1
	}
}

// MoreRestrictive returns the tighter of two concrete settings. Inherit is
// treated as no-opinion and loses to any concrete setting.
func MoreRestrictive(a, b Setting) Setting {
	if rank(a) >= rank(b) {
		if rank(a) < 0 {
			return SettingInherit
		}
		return a
	}
	return b
}

// Input is the resolved per-layer chain for one tool and one (user, agent,
// runtime) context. A layer absent from Settings is treated as Inherit.
//
// Group membership is collapsed into a single LayerGroup setting before it
// reaches Resolve; use CombineGroups for the multi-group case.
type Input struct {
	// Settings holds the explicit choice at each layer. Missing == Inherit.
	Settings map[Layer]Setting
	// Base is the value in force below the runtime layer — the workspace/system
	// default applied when even the runtime layer inherits. An empty or Inherit
	// Base defaults to Allow.
	Base Setting
	// IsSystem marks this as a run driven by a System actor (autopilot, no human
	// behind it). A System has no one to answer an approval prompt, so any Ask it
	// would resolve to is treated as Deny — fail-safe (FIR-1609). Default false
	// preserves existing human-run behavior for every current caller.
	IsSystem bool
}

// Effective is the resolved verdict for one tool.
type Effective struct {
	// Setting is the combined result: always Allow, Ask, or Deny (never Inherit).
	Setting Setting
	// DecidedBy is the layer responsible for the effective setting — the highest
	// layer (closest to the ceiling) whose explicit setting equals the final
	// restrictiveness. Empty when no layer had an opinion and Base decided.
	DecidedBy Layer
	// CappedBy is set when a layer above the base tightened access below what the
	// runtime base offered — i.e. the effective setting is more restrictive than
	// the base value because of this layer. It drives the "Capped by …" badge.
	// Empty when the base value was not tightened by a higher layer.
	CappedBy Layer
	// Reason is a short human-readable explanation suitable for audit rows and
	// tooltips, e.g. "Capped by user" or "Allowed by runtime default".
	Reason string
}

// Allowed reports whether the tool may run without an approval pause.
func (e Effective) Allowed() bool { return e.Setting == SettingAllow }

// Resolve folds the chain into a single Effective verdict.
//
// Algorithm (walk base → ceiling):
//
//  1. Start from Base (the system/workspace default below the runtime).
//  2. For each layer, an Inherit setting passes the running value through.
//  3. A concrete setting can only tighten: the running value becomes the more
//     restrictive of (running, layer). A layer trying to loosen is ignored.
//  4. On a tie at the current restrictiveness, the higher layer becomes the
//     decider, so attribution flows toward the ceiling.
//
// Because step 3 is a monotonic max over restrictiveness, the effective setting
// is independent of layer order; only DecidedBy / CappedBy depend on the walk.
func Resolve(in Input) Effective {
	base := in.Base
	if rank(base) < 0 {
		base = SettingAllow
	}

	resolved := base
	baseRank := rank(base)
	var decidedBy Layer
	var cappedBy Layer

	for _, layer := range chainOrder {
		v := in.Settings[layer]
		if rank(v) < 0 {
			continue // Inherit / absent — follow the layer below.
		}
		switch {
		case rank(v) > rank(resolved):
			// This layer tightens the running value.
			resolved = v
			decidedBy = layer
			// A layer above the base infrastructure (workspace + runtime) that
			// pushes the result tighter than the base offered is "capping" access.
			// Workspace and runtime are root defaults — they set the starting
			// point, they do not "cap".
			if layer != LayerRuntime && layer != LayerWorkspace && rank(v) > baseRank {
				cappedBy = layer
			}
		case rank(v) == rank(resolved):
			// Same restrictiveness — the higher layer owns the attribution.
			decidedBy = layer
		default:
			// Loosening is not permitted; the running value stands.
		}
	}

	// A System actor (autopilot, no human) cannot answer an approval prompt, so
	// an Ask it would otherwise resolve to becomes Deny — fail-safe (FIR-1609).
	// This is a property of the actor, not of any one layer, so it applies even
	// when the Ask came from a lower layer and no System setting was authored.
	if in.IsSystem && resolved == SettingAsk {
		return Effective{
			Setting:   SettingDeny,
			DecidedBy: LayerSystem,
			CappedBy:  LayerSystem,
			Reason:    "Denied — system actor has no human to answer an Ask",
		}
	}

	return Effective{
		Setting:   resolved,
		DecidedBy: decidedBy,
		CappedBy:  cappedBy,
		Reason:    reasonFor(resolved, decidedBy, cappedBy),
	}
}

// ResolveMemberOverride folds the chain using the member-override model
// (FIR-2175): a two-stage resolution that matches how an operator intuitively
// reasons about access.
//
//	Stage A — the MEMBER decides by specificity. Among the human-facing layers
//	  Workspace › Group › User, the MOST SPECIFIC explicit setting wins
//	  (User over Group over Workspace). Unlike Resolve, this stage may LOOSEN as
//	  well as tighten: a User (member) Allow overrides a Group Deny. A member's
//	  own setting is authoritative for the member; group/workspace are the
//	  inherited defaults it can override.
//	Stage B — the AGENT inherits the member verdict as a CEILING, then Runtime,
//	  Agent (and System) may only TIGHTEN it. An agent can never do more than its
//	  member; a Deny on runtime/agent wins even when member/workspace say Allow.
//
// CONTRAST WITH Resolve: Resolve is pure most-restrictive-wins (tighten-only at
// every layer) and is the load-bearing invariant for the deny-by-default gates —
// credentials, the OS sandbox, repo checkout, the approval cap. Because this
// function can LOOSEN (member overrides group), it MUST NOT be used to gate any
// deny-by-default floor. It is for the general tool-policy chain only, behind the
// member-override feature flag. Keep credentials/sandbox/etc. on Resolve.
func ResolveMemberOverride(in Input) Effective {
	base := in.Base
	if rank(base) < 0 {
		base = SettingAllow
	}

	// Stage A — member effective by specificity: the most specific explicit
	// layer wins (User > Group > Workspace). Iterating broad→specific and
	// overwriting on each explicit layer leaves the most specific one in force.
	memberEff := base
	var memberDecidedBy Layer
	for _, layer := range []Layer{LayerWorkspace, LayerGroup, LayerUser} {
		if v := in.Settings[layer]; rank(v) >= 0 {
			memberEff = v
			memberDecidedBy = layer
		}
	}

	// Stage B — the agent inherits the member ceiling; runtime/agent/system may
	// only tighten it. A layer trying to loosen is ignored.
	resolved := memberEff
	decidedBy := memberDecidedBy
	var cappedBy Layer
	memberRank := rank(memberEff)
	for _, layer := range []Layer{LayerRuntime, LayerAgent, LayerSystem} {
		v := in.Settings[layer]
		if rank(v) < 0 {
			continue // Inherit / absent — no opinion.
		}
		switch {
		case rank(v) > rank(resolved):
			resolved = v
			decidedBy = layer
			if rank(v) > memberRank {
				cappedBy = layer // tightened the agent below what the member allowed.
			}
		case rank(v) == rank(resolved):
			decidedBy = layer
		default:
			// Loosening is not permitted on the agent side; member is the ceiling.
		}
	}

	// A System actor (autopilot, no human) cannot answer an approval prompt, so
	// an Ask it would resolve to becomes Deny — fail-safe (FIR-1609), identical
	// to Resolve.
	if in.IsSystem && resolved == SettingAsk {
		return Effective{
			Setting:   SettingDeny,
			DecidedBy: LayerSystem,
			CappedBy:  LayerSystem,
			Reason:    "Denied — system actor has no human to answer an Ask",
		}
	}

	return Effective{
		Setting:   resolved,
		DecidedBy: decidedBy,
		CappedBy:  cappedBy,
		Reason:    reasonFor(resolved, decidedBy, cappedBy),
	}
}

// ResolveOptIn decides an OFF-by-default capability gate from the explicit
// per-layer settings. It exists because Resolve (the tighten-only chain) cannot
// express opt-in: Resolve only ever TIGHTENS below its Base and refuses to
// loosen ("Loosening is not permitted; the running value stands"), so a Deny
// Base can never be lifted by a grant — Resolve(Base: Deny) is ALWAYS Deny no
// matter what Allow rows exist below it. An opt-in capability is the opposite
// shape: nobody holds it until an admin grants it. Semantics here:
//
//   - granted by an explicit Allow at the User OR Group layer;
//   - an explicit User Deny revokes it even when a group grants it;
//   - everything else (no rows, Ask, Inherit) stays OFF.
//
// LayerGroup has already been collapsed to its single most-permissive value by
// CombineGroups before it reaches here, so a User with no row inherits any
// group grant. Used by capability gates such as tools:test-as-user (FIR-1771).
func ResolveOptIn(in Input) bool {
	if in.Settings[LayerUser] == SettingDeny {
		return false
	}
	return in.Settings[LayerUser] == SettingAllow || in.Settings[LayerGroup] == SettingAllow
}

// CombineGroups collapses the settings from every group a user belongs to into
// the single value that enters Resolve at LayerGroup.
//
// Group membership is additive below the ceiling: belonging to a more permissive
// group should not be punished by a stricter sibling group, so the LEAST
// restrictive explicit group setting wins. The user ceiling above still caps the
// result. Groups with no opinion (Inherit / absent) do not constrain.
func CombineGroups(settings ...Setting) Setting {
	combined := SettingInherit
	combinedRank := -1
	for _, s := range settings {
		r := rank(s)
		if r < 0 {
			continue
		}
		if combinedRank < 0 || r < combinedRank {
			combined = s
			combinedRank = r
		}
	}
	return combined
}

func reasonFor(setting Setting, decidedBy, cappedBy Layer) string {
	if cappedBy != "" {
		return fmt.Sprintf("Capped by %s", cappedBy)
	}
	if decidedBy == "" {
		return fmt.Sprintf("%s by default", labelFor(setting))
	}
	return fmt.Sprintf("%s by %s", labelFor(setting), decidedBy)
}

func labelFor(setting Setting) string {
	switch setting {
	case SettingAllow:
		return "Allowed"
	case SettingAsk:
		return "Ask"
	case SettingDeny:
		return "Denied"
	default:
		return string(setting)
	}
}

// Package localtoolpolicy holds the pure, IO-free half of per-tool enforcement
// for LOCAL CLI runtimes (Claude, Codex, Cursor, Gemini) that run on a Mac via
// the daemon. The gateway executor already enforces the unified tool-policy
// chain through guardToolCallViaPolicy (server/internal/cerebro/runtime/
// approval_gate.go); local CLIs never pass through that choke-point, so this
// package is the shared decision logic the daemon-side resolve endpoint folds a
// resolved toolpolicy.Effective verdict through before acting on it.
//
// It is the SAME model, not a parallel one: resolution uses toolpolicy.Store,
// and an "Ask" verdict lands in the SAME approvals inbox the gateway gate uses.
// The only thing this package adds on top of the gateway path is the staged
// rollout selector (Mode) the security requirement demands — observe before
// enforce, default off — so local enforcement can be switched on machine-wide
// without a big-bang and without ever locking an agent out by default.
//
// The split mirrors toolpolicy.Resolve (pure) vs Store (IO): Decide is a pure
// fold of (Mode, Effective) → the verdict the daemon hook acts on, exhaustively
// unit-testable without a database or HTTP. The handler seam in
// server/internal/handler/daemon_tool_policy_cerebro.go does the IO (resolve,
// create the inbox ask) and is the only part that touches the network.
package localtoolpolicy

import (
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// Mode is the staged rollout selector for local-runtime per-tool enforcement.
// It governs ONLY whether a resolved verdict is acted on, never how the verdict
// is computed — the verdict always comes from the same toolpolicy chain.
type Mode string

const (
	// ModeOff is the default: the resolve endpoint short-circuits to allow and
	// never resolves, so spawning a local CLI behaves exactly as before. No
	// agent is locked out, nothing is logged, the inbox is untouched.
	ModeOff Mode = "off"
	// ModeObserve resolves every gated tool call and LOGS what an enforce would
	// have blocked, but always allows the call to proceed. This is the dry-run
	// stage: an operator can watch the would-block stream for a machine before
	// flipping to enforce, with zero risk of stalling real work.
	ModeObserve Mode = "observe"
	// ModeEnforce actually acts on the verdict: Allow proceeds, Deny stops, and
	// Ask creates an inbox approval request and blocks the tool until a human
	// decides — full parity with the gateway gate.
	ModeEnforce Mode = "enforce"
)

// EnvVar is the environment variable the daemon-side endpoint reads to pick the
// stage. It is deliberately distinct from CEREBRO_APPROVAL_GATE_ENABLED (which
// arms the gateway gate) because local-runtime rollout is staged on its own
// timeline; both still resolve through the one tool-policy chain and share the
// one approvals inbox.
const EnvVar = "CEREBRO_LOCAL_TOOL_POLICY_MODE"

// ParseMode maps a raw string to a Mode, defaulting to ModeOff for empty or
// unrecognised values so a typo can never silently start blocking agents.
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "observe", "dry-run", "dryrun", "log":
		return ModeObserve
	case "enforce", "on", "block":
		return ModeEnforce
	default:
		return ModeOff
	}
}

// ModeFromEnv reads EnvVar and returns the configured stage (ModeOff by default).
//
// DEPRECATED as the rollout control: the daemon endpoint now reads the stage
// from workspace settings (the cerebro feature-flag model), not the environment,
// so an admin can stage the rollout from Settings → Features with no server
// restart (Jesper's requirement, TECH-3173). Kept only so the symbol and the
// env-var escape hatch remain available; ResolveDaemonToolPolicy uses
// ModeFromFlags instead.
func ModeFromEnv() Mode {
	return ParseMode(os.Getenv(EnvVar))
}

// ModeFromFlags folds the two workspace settings that stage the local-runtime
// rollout into a Mode. This is the production control (see ResolveDaemonToolPolicy):
//
//   - enabled=false                  → ModeOff     (default; no behaviour change)
//   - enabled=true,  enforce=false   → ModeObserve (dry run: resolve + log, allow)
//   - enabled=true,  enforce=true    → ModeEnforce (act on the verdict)
//
// Both settings default OFF, so a fresh deploy stays at ModeOff until an admin
// opts in from Settings — the same staged, default-safe shape the env var had,
// but toggled from the UI instead of the environment.
func ModeFromFlags(enabled, enforce bool) Mode {
	if !enabled {
		return ModeOff
	}
	if enforce {
		return ModeEnforce
	}
	return ModeObserve
}

// DecisionKind is the action the daemon hook must take for one tool call.
type DecisionKind string

const (
	// KindAllow: let the tool run.
	KindAllow DecisionKind = "allow"
	// KindDeny: block the tool outright (no human in the loop).
	KindDeny DecisionKind = "deny"
	// KindAsk: the verdict is "ask" and the stage is enforce — the handler must
	// create (or rejoin) an inbox approval request and block until a human
	// decides. Only Decide in ModeEnforce returns this; observe downgrades it to
	// an allow-with-would-block.
	KindAsk DecisionKind = "ask"
)

// Decision is the pure outcome of folding a Mode over a resolved verdict. It is
// everything the daemon hook needs except the inbox round-trip, which the
// handler performs only when Kind == KindAsk.
type Decision struct {
	// Kind is the action to take. KindAsk additionally requires the handler to
	// open an inbox approval and poll it.
	Kind DecisionKind
	// Allowed is the immediate answer the hook acts on for non-Ask kinds. For
	// KindAsk it is false (the call is held pending the human).
	Allowed bool
	// Enforced reports whether this decision is binding (enforce) or advisory
	// (observe/off). The hook uses it only for telemetry; it always honours
	// Allowed.
	Enforced bool
	// WouldBlock reports whether an enforce stage would have stopped or paused
	// this call. In observe it is the whole point of the dry run; in enforce it
	// equals !Allowed-on-allow. It feeds the observe would-block log.
	WouldBlock bool
	// Observed is the raw resolved setting (allow/ask/deny) regardless of stage,
	// so observe logging and audit rows can record what the chain decided
	// independent of whether it was acted on.
	Observed toolpolicy.Setting
	// Reason carries the chain's human-readable attribution (e.g. "Capped by
	// user") through to the hook and the inbox ask.
	Reason string
}

// NeedsApproval reports whether the handler must open an inbox approval for this
// decision (true only for an enforce-stage Ask verdict).
func (d Decision) NeedsApproval() bool { return d.Kind == KindAsk }

// Decide folds a rollout Mode over a resolved tool-policy verdict into the
// action the daemon hook takes. It is pure — no IO, no clock, no randomness —
// so every (mode, setting) cell is unit-tested directly.
//
// Truth table:
//
//	mode \ setting   allow            ask                        deny
//	off              allow            allow                      allow
//	observe          allow            allow (would-block)        allow (would-block)
//	enforce          allow            ASK (inbox + block)        deny
//
// An unknown/empty Effective.Setting is treated as Deny under enforce
// (fail-closed) and as a would-block under observe, mirroring the gateway's
// "needs_approval is never silently downgraded to allow" stance.
func Decide(mode Mode, eff toolpolicy.Effective) Decision {
	setting := eff.Setting
	base := Decision{Observed: setting, Reason: eff.Reason}

	switch mode {
	case ModeEnforce:
		switch setting {
		case toolpolicy.SettingAllow:
			base.Kind = KindAllow
			base.Allowed = true
			base.Enforced = true
		case toolpolicy.SettingAsk:
			base.Kind = KindAsk
			base.Allowed = false
			base.Enforced = true
			base.WouldBlock = true
		default: // Deny, Inherit, or any unexpected value → fail-closed.
			base.Kind = KindDeny
			base.Allowed = false
			base.Enforced = true
			base.WouldBlock = true
		}
		return base
	case ModeObserve:
		base.Kind = KindAllow
		base.Allowed = true
		base.Enforced = false
		base.WouldBlock = setting != toolpolicy.SettingAllow
		return base
	default: // ModeOff and any unknown mode.
		base.Kind = KindAllow
		base.Allowed = true
		base.Enforced = false
		base.WouldBlock = false
		return base
	}
}

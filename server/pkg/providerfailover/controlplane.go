package providerfailover

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Control-plane effect idempotency (td-836aa9). An ORCHESTRATOR-tier run does
// not just answer a prompt; it dispatches CONTROL-PLANE effects — it spawns
// child tasks/issues and promotes stages. If such a run is handed off
// mid-orchestration, the replacement runtime re-plans from scratch on top of
// whatever the primary already did, so without a guard it would re-spawn the
// same children and re-promote the same stages: a double-dispatch.
//
// The fix is an idempotency ledger keyed by a deterministic EffectKey. Before a
// control-plane effect is applied, the caller claims its key; a second claim of
// the same (chain, effect, target) is rejected, so the effect happens
// at-most-once across the original run AND any fallback. This package owns the
// pure key derivation; server/internal/service owns the durable claim (see
// ClaimControlPlaneEffectOnce) against the control_plane_effect_ledger table.

// ControlPlaneEffect is the kind of orchestration side effect being made
// idempotent. The string form is persisted in the ledger and embedded in the
// idempotency key, so renaming one is a breaking change (it would orphan prior
// claims and re-enable a double-dispatch for in-flight chains).
type ControlPlaneEffect string

const (
	// EffectTaskSpawn: the orchestrator created a child task / sub-issue. Target
	// is the stable identity of the thing spawned (e.g. the child issue id, or a
	// caller-chosen dedup token for a not-yet-persisted spawn).
	EffectTaskSpawn ControlPlaneEffect = "task_spawn"
	// EffectStagePromotion: the orchestrator promoted a stage of a parent issue.
	// Target is the parent issue id + stage number, so promoting stage 2 twice
	// collapses to one effect but promoting stage 3 is a distinct, allowed one.
	EffectStagePromotion ControlPlaneEffect = "stage_promotion"
)

// EffectKey derives the deterministic idempotency key for a control-plane
// effect. The key is stable across runs and providers so the ORIGINAL run and a
// handed-off FALLBACK run compute the identical key for the same logical effect
// and therefore contend for the same at-most-once claim.
//
// Composition: the chain root (the failover ownership unit), the effect kind,
// and the effect's target identity, hashed so an arbitrarily-long or delimiter-
// bearing target can never collide with or inject into another key. An empty
// chainRoot or target yields "" — an unkeyable effect the caller must treat as
// NOT idempotency-protected (fail-closed: it should decline to proceed under an
// orchestrator handoff rather than risk a double-dispatch on an unkeyable
// effect).
func EffectKey(chainRoot string, effect ControlPlaneEffect, target string) string {
	chainRoot = strings.TrimSpace(chainRoot)
	target = strings.TrimSpace(target)
	if chainRoot == "" || effect == "" || target == "" {
		return ""
	}
	// Length-prefix each component so no combination of internal delimiters can
	// make two distinct (chainRoot, effect, target) triples hash equal.
	h := sha256.New()
	for _, part := range []string{chainRoot, string(effect), target} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0}) // NUL separator, impossible in the UUID/text inputs
	}
	return hex.EncodeToString(h.Sum(nil))
}

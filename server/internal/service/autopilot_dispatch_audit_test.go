package service

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestDispatchAuditReason pins the reason taxonomy: a plain failover, a
// half-open probe on the primary (no fallback), and a failover that lands on a
// probe window are three distinct routing events an operator must tell apart.
func TestDispatchAuditReason(t *testing.T) {
	cases := []struct {
		name     string
		fellBack bool
		chosen   runtimeCandidate
		want     string
	}{
		{"plain failover", true, runtimeCandidate{}, "runtime_failover"},
		{"failover onto probe window", true, runtimeCandidate{ProbeWindow: true}, "failover_half_open_probe"},
		{"probe on primary, no fallback", false, runtimeCandidate{ProbeWindow: true}, "half_open_probe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dispatchAuditReason(tc.fellBack, tc.chosen); got != tc.want {
				t.Fatalf("dispatchAuditReason(%v, probe=%v) = %q, want %q", tc.fellBack, tc.chosen.ProbeWindow, got, tc.want)
			}
		})
	}
}

// TestBuildDispatchRuntimeAudit proves the never-silent evidence (F6) carries
// every field the card enumerates: source/target runtime+provider, the ordered
// pool with per-binding priority, chosen flag, hold class, hold-until, reset
// source and circuit generation. A held candidate with no deadline must surface
// an empty hold_until (never a fabricated date, I9), and the model_action is
// left for the daemon to merge later (empty at dispatch).
func TestBuildDispatchRuntimeAudit(t *testing.T) {
	sourceID := mustUUID(t, "11111111-1111-1111-1111-111111111111")
	targetID := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	heldNoDeadline := mustUUID(t, "33333333-3333-3333-3333-333333333333")
	holdUntil := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	candidates := []runtimeCandidate{
		{
			RuntimeID:   sourceID,
			Provider:    "claude",
			Priority:    0,
			Held:        true,
			HeldClass:   circuitClassQuota,
			HoldUntil:   holdUntil,
			ResetSource: circuitResetParseable,
			Generation:  7,
		},
		{
			RuntimeID:  targetID,
			Provider:   "codex",
			Priority:   1,
			Available:  true,
			Generation: 3,
		},
		{
			RuntimeID: heldNoDeadline,
			Provider:  "opencode",
			Priority:  2,
			Held:      true,
			HeldClass: circuitClassQuota,
			// zero HoldUntil: an open circuit whose reset time is unknown.
		},
	}
	chosen := candidates[1]

	audit := buildDispatchRuntimeAudit("runtime_failover", sourceID, chosen, candidates)

	if audit.Reason != "runtime_failover" {
		t.Errorf("reason = %q, want runtime_failover", audit.Reason)
	}
	if audit.SourceRuntimeID != util.UUIDToString(sourceID) {
		t.Errorf("source runtime id = %q, want %q", audit.SourceRuntimeID, util.UUIDToString(sourceID))
	}
	if audit.SourceProvider != "claude" {
		t.Errorf("source provider = %q, want claude (resolved from the pool row)", audit.SourceProvider)
	}
	if audit.TargetRuntimeID != util.UUIDToString(targetID) {
		t.Errorf("target runtime id = %q, want %q", audit.TargetRuntimeID, util.UUIDToString(targetID))
	}
	if audit.TargetProvider != "codex" {
		t.Errorf("target provider = %q, want codex", audit.TargetProvider)
	}
	if audit.TargetPriority != 1 {
		t.Errorf("target priority = %d, want 1", audit.TargetPriority)
	}
	if audit.ModelAction != "" {
		t.Errorf("model_action = %q, want empty at dispatch (daemon merges it later)", audit.ModelAction)
	}
	if len(audit.Pool) != 3 {
		t.Fatalf("pool has %d items, want 3 (full ordered pool)", len(audit.Pool))
	}

	// Source row: held quota, parseable reset, generation and an RFC3339 deadline.
	src := audit.Pool[0]
	if src.Priority != 0 || !src.Held || src.Chosen {
		t.Errorf("source pool item = %+v, want priority 0, held, not chosen", src)
	}
	if src.HeldClass != circuitClassQuota || src.ResetSource != circuitResetParseable || src.Generation != 7 {
		t.Errorf("source hold facts = %+v, want quota/parseable/gen7", src)
	}
	if src.HoldUntil != holdUntil.Format(time.RFC3339) {
		t.Errorf("source hold_until = %q, want %q", src.HoldUntil, holdUntil.Format(time.RFC3339))
	}

	// Target row: the chosen binding.
	tgt := audit.Pool[1]
	if !tgt.Chosen || tgt.Held || tgt.Provider != "codex" || tgt.Generation != 3 {
		t.Errorf("target pool item = %+v, want chosen, unheld, codex, gen3", tgt)
	}

	// Held-with-no-deadline row: empty hold_until, never a fabricated date (I9).
	nd := audit.Pool[2]
	if !nd.Held || nd.HoldUntil != "" {
		t.Errorf("no-deadline pool item = %+v, want held with empty hold_until", nd)
	}
}

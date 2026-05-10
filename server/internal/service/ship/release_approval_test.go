// Phase 7d follow-up — unit tests for the configurable approval gate.
//
// The four-rule eligibility check (member / admin / approver / two)
// is pure: it takes a release row + caller UUID + ApprovalContext and
// returns (slot, ok). No DB, no clock, no goroutines. The integration
// path (signoff persistence + ErrTwoApproverPending semantics) is
// covered by the handler-package tests against the real Postgres
// pool; this file pins down the rule semantics in isolation so a
// regression in approverEligibility points at exactly one function.

package ship

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uuidFromByte builds a deterministic UUID from a single byte so the
// tests can compose distinct identities without a uuid generator.
// The byte populates the first slot of the 16-byte array; the rest
// stays zero. Two different bytes yield two unequal UUIDs.
func uuidFromByte(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{b}, Valid: true}
}

// release is a tiny fixture builder so test cases stay readable.
func releaseFixture(risk db.RiskLevel, approver, secondApprover pgtype.UUID) db.ShipRelease {
	return db.ShipRelease{
		ID:               uuidFromByte(0xFF),
		WorkspaceID:      uuidFromByte(0xFE),
		RiskLevel:        risk,
		ApproverID:       approver,
		SecondApproverID: secondApprover,
	}
}

// TestResolveApprovalRule_FallbacksMatchLegacy — an empty / unknown
// rule string falls back to the original hardcoded behavior so a
// pre-migration workspace continues to work without a backfill.
func TestResolveApprovalRule_FallbacksMatchLegacy(t *testing.T) {
	cases := []struct {
		risk db.RiskLevel
		want string
	}{
		{db.RiskLevelLow, ApprovalRuleMember},
		{db.RiskLevelMedium, ApprovalRuleMember},
		{db.RiskLevelHigh, ApprovalRuleApprover},
		{db.RiskLevelCritical, ApprovalRuleTwo},
	}
	for _, c := range cases {
		t.Run(string(c.risk), func(t *testing.T) {
			if got := resolveApprovalRule("", c.risk); got != c.want {
				t.Errorf("empty rule fallback for %s: got %q, want %q", c.risk, got, c.want)
			}
			if got := resolveApprovalRule("garbage", c.risk); got != c.want {
				t.Errorf("garbage rule fallback for %s: got %q, want %q", c.risk, got, c.want)
			}
		})
	}
}

// TestApproverEligibility_MemberRule — "member" lets any caller in
// (workspace membership is the floor, gated by the handler).
func TestApproverEligibility_MemberRule(t *testing.T) {
	caller := uuidFromByte(1)
	rel := releaseFixture(db.RiskLevelLow, pgtype.UUID{}, pgtype.UUID{})
	_, ok := approverEligibility(ApprovalRuleMember, rel, caller, ApprovalContext{
		MemberRole: "member",
	})
	if !ok {
		t.Fatalf("member rule: expected eligible, got false")
	}
}

// TestApproverEligibility_AdminRule — only owner/admin pass.
func TestApproverEligibility_AdminRule(t *testing.T) {
	caller := uuidFromByte(1)
	rel := releaseFixture(db.RiskLevelLow, pgtype.UUID{}, pgtype.UUID{})

	if _, ok := approverEligibility(ApprovalRuleAdmin, rel, caller, ApprovalContext{
		MemberRole: "member",
	}); ok {
		t.Errorf("admin rule: a plain member should be denied")
	}
	if _, ok := approverEligibility(ApprovalRuleAdmin, rel, caller, ApprovalContext{
		MemberRole: "admin",
	}); !ok {
		t.Errorf("admin rule: an admin should be allowed")
	}
	if _, ok := approverEligibility(ApprovalRuleAdmin, rel, caller, ApprovalContext{
		MemberRole: "owner",
	}); !ok {
		t.Errorf("admin rule: an owner should be allowed")
	}
}

// TestApproverEligibility_ApproverRule — only release.approver_id or
// a workspace admin passes.
func TestApproverEligibility_ApproverRule(t *testing.T) {
	approver := uuidFromByte(1)
	other := uuidFromByte(2)
	rel := releaseFixture(db.RiskLevelHigh, approver, pgtype.UUID{})

	// Approver match.
	if _, ok := approverEligibility(ApprovalRuleApprover, rel, approver, ApprovalContext{
		MemberRole: "member",
	}); !ok {
		t.Errorf("approver rule: the designated approver should be allowed")
	}
	// Non-approver, non-admin.
	if _, ok := approverEligibility(ApprovalRuleApprover, rel, other, ApprovalContext{
		MemberRole: "member",
	}); ok {
		t.Errorf("approver rule: a random member should be denied")
	}
	// Admin override (not the approver, but an admin).
	if _, ok := approverEligibility(ApprovalRuleApprover, rel, other, ApprovalContext{
		MemberRole: "admin",
	}); !ok {
		t.Errorf("approver rule: a workspace admin should be allowed")
	}
}

// TestApproverEligibility_TwoRule_SlotMatching — confirms slot
// resolution: primary approver → first, secondary approver → second,
// admin without approver match → first.
func TestApproverEligibility_TwoRule_SlotMatching(t *testing.T) {
	primary := uuidFromByte(1)
	secondary := uuidFromByte(2)
	rel := releaseFixture(db.RiskLevelCritical, primary, secondary)

	slot, ok := approverEligibility(ApprovalRuleTwo, rel, primary, ApprovalContext{
		MemberRole: "member",
	})
	if !ok || slot != SignoffSlotFirst {
		t.Errorf("two rule + primary: expected first/ok, got %s/%v", slot, ok)
	}

	slot, ok = approverEligibility(ApprovalRuleTwo, rel, secondary, ApprovalContext{
		MemberRole: "member",
	})
	if !ok || slot != SignoffSlotSecond {
		t.Errorf("two rule + secondary: expected second/ok, got %s/%v", slot, ok)
	}

	other := uuidFromByte(3)
	if _, ok := approverEligibility(ApprovalRuleTwo, rel, other, ApprovalContext{
		MemberRole: "member",
	}); ok {
		t.Errorf("two rule + random member: expected denied, got allowed")
	}

	slot, ok = approverEligibility(ApprovalRuleTwo, rel, other, ApprovalContext{
		MemberRole: "admin",
	})
	if !ok || slot != SignoffSlotFirst {
		t.Errorf("two rule + admin (no approver match): expected first/ok, got %s/%v", slot, ok)
	}
}

// TestApproverEligibility_AuthorSeparationOfDuties — when the
// workspace has flipped off "approver can be author" AND the caller
// is in the PR-author set, the gate denies even if they'd otherwise
// pass.
func TestApproverEligibility_AuthorSeparationOfDuties(t *testing.T) {
	approver := uuidFromByte(1)
	rel := releaseFixture(db.RiskLevelHigh, approver, pgtype.UUID{})

	if _, ok := approverEligibility(ApprovalRuleApprover, rel, approver, ApprovalContext{
		MemberRole:  "member",
		IsAuthor:    true,
		CanBeAuthor: false,
	}); ok {
		t.Errorf("expected denial when CanBeAuthor=false and IsAuthor=true")
	}
	// Same shape, but CanBeAuthor=true → allow.
	if _, ok := approverEligibility(ApprovalRuleApprover, rel, approver, ApprovalContext{
		MemberRole:  "member",
		IsAuthor:    true,
		CanBeAuthor: true,
	}); !ok {
		t.Errorf("expected allow when CanBeAuthor=true and IsAuthor=true")
	}
}

// TestApproverEligibility_ZeroCaller_FailsClosed — an unauthenticated
// caller (zero UUID) is rejected unconditionally.
func TestApproverEligibility_ZeroCaller_FailsClosed(t *testing.T) {
	rel := releaseFixture(db.RiskLevelLow, pgtype.UUID{}, pgtype.UUID{})
	if _, ok := approverEligibility(ApprovalRuleMember, rel, pgtype.UUID{}, ApprovalContext{
		MemberRole: "member",
	}); ok {
		t.Errorf("zero requestedBy should fail closed even on member rule")
	}
}

// TestBothSlotsSigned_RequiresDistinctSigners — two signoff rows
// from the SAME user fail (separation-of-duties); two distinct users
// pass.
func TestBothSlotsSigned_RequiresDistinctSigners(t *testing.T) {
	a := uuidFromByte(1)
	b := uuidFromByte(2)

	// Same user in both slots — fail.
	rows := []db.ShipReleaseSignoff{
		{ApproverSlot: SignoffSlotFirst, SignedBy: a},
		{ApproverSlot: SignoffSlotSecond, SignedBy: a},
	}
	if bothSlotsSigned(rows) {
		t.Errorf("same signer in both slots: expected not satisfied")
	}

	// Distinct signers — pass.
	rows = []db.ShipReleaseSignoff{
		{ApproverSlot: SignoffSlotFirst, SignedBy: a},
		{ApproverSlot: SignoffSlotSecond, SignedBy: b},
	}
	if !bothSlotsSigned(rows) {
		t.Errorf("distinct signers: expected satisfied")
	}

	// Only one slot — fail.
	rows = []db.ShipReleaseSignoff{
		{ApproverSlot: SignoffSlotFirst, SignedBy: a},
	}
	if bothSlotsSigned(rows) {
		t.Errorf("only first slot: expected not satisfied")
	}
}

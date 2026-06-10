package commentguard

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// fakeFlags is a flagReader stub. The enabled map controls which workspace
// feature flags are reported as on; err forces a lookup error.
type fakeFlags struct {
	flags map[string]bool
	err   error
}

// legacyFakeFlags builds a fakeFlags from the original single-bool form used
// in the pre-TECH-3099 tests — enables only FlagCommentTargetGuard.
func legacyFakeFlags(enabled bool) fakeFlags {
	if !enabled {
		return fakeFlags{}
	}
	return fakeFlags{flags: map[string]bool{FlagCommentTargetGuard: true}}
}

func (f fakeFlags) ListCerebroWorkspaceFeatureFlags(_ context.Context, _ pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	var rows []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	for key, enabled := range f.flags {
		rows = append(rows, cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{FlagKey: key, Enabled: enabled})
	}
	return rows, nil
}

func testWorkspaceID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan("11bd8321-b6ac-4bee-ae41-6659a5064608"); err != nil {
		t.Fatalf("scan workspace id: %v", err)
	}
	return id
}

// With the flag ON, the rejection logic behaves as before.
func TestRejectCommentFlagOn(t *testing.T) {
	g := New(legacyFakeFlags(true))
	ws := testWorkspaceID(t)

	cases := []struct {
		name       string
		authorType string
		content    string
		wantOK     bool
	}{
		{"member with no target passes", "member", "just a note", true},
		{"agent with no target rejected", "agent", "work is done", false},
		{"agent with empty content rejected", "agent", "", false},
		{"agent with member mention passes", "agent", "done [@Jesper](mention://member/b0edd870-4ea2-4638-a193-5c20f55170e6)", true},
		{"agent with only an issue link rejected", "agent", "see [MUL-123](mention://issue/10fb2c2c-1ec4-449d-a081-b690ef70eb17)", false},
		{"agent with issue link plus a recipient passes", "agent", "see [MUL-123](mention://issue/10fb2c2c-1ec4-449d-a081-b690ef70eb17) [@Jesper](mention://member/b0edd870-4ea2-4638-a193-5c20f55170e6)", true},
		{"agent with agent mention passes", "agent", "[@Tine](mention://agent/8bfccab3-89ea-40cc-b57d-c7eabaa30f50) please test", true},
		{"agent with squad mention passes", "agent", "[@Squad](mention://squad/8bfccab3-89ea-40cc-b57d-c7eabaa30f50)", true},
		{"agent with all mention passes", "agent", "[@all](mention://all/all)", true},
		{"agent text that merely looks like a mention is rejected", "agent", "talk to @Jesper soon", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := g.RejectComment(context.Background(), ws, tc.authorType, tc.content, false, nil, false)
			if ok != tc.wantOK {
				t.Fatalf("RejectComment(%q, %q) ok=%v, want %v", tc.authorType, tc.content, ok, tc.wantOK)
			}
			if !ok && msg == "" {
				t.Fatalf("rejected comment must carry a message")
			}
			if ok && msg != "" {
				t.Fatalf("passing comment must not carry a message, got %q", msg)
			}
		})
	}
}

// With the flag OFF (the default — no override row), even an agent comment with
// no target passes: the guard does nothing until an admin turns the flag on.
func TestRejectCommentFlagOffPassesEverything(t *testing.T) {
	g := New(legacyFakeFlags(false))
	ws := testWorkspaceID(t)
	if msg, ok := g.RejectComment(context.Background(), ws, "agent", "no target here", false, nil, false); !ok || msg != "" {
		t.Fatalf("flag off must pass everything, got ok=%v msg=%q", ok, msg)
	}
}

// A nil Service, or one built with a nil reader, is a safe no-op.
func TestRejectCommentNilSafe(t *testing.T) {
	ws := testWorkspaceID(t)

	var nilService *Service
	if msg, ok := nilService.RejectComment(context.Background(), ws, "agent", "no target", false, nil, false); !ok || msg != "" {
		t.Fatalf("nil service must pass everything, got ok=%v msg=%q", ok, msg)
	}

	nilReader := New(nil)
	if msg, ok := nilReader.RejectComment(context.Background(), ws, "agent", "no target", false, nil, false); !ok || msg != "" {
		t.Fatalf("nil reader must pass everything, got ok=%v msg=%q", ok, msg)
	}
}

// TECH-3099: sub-issue checks.

// allSubIssueFlags builds a fakeFlags with all guard flags on.
func allSubIssueFlags() fakeFlags {
	return fakeFlags{flags: map[string]bool{
		FlagCommentTargetGuard:      true,
		FlagSubIssueNoOwnerMention:  true,
		FlagSubIssueRequireAgentTag: true,
		FlagSubIssueNoSplitSession:  true,
	}}
}

const ownerUserID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
const agentID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
const issueRef = "[MUL-1](mention://issue/cccccccc-cccc-cccc-cccc-cccccccccccc)"
const agentMention = "[@Tine](mention://agent/" + agentID + ")"
const ownerMention = "[@Jesper](mention://member/" + ownerUserID + ")"

// memberMention is a non-owner member — a valid recipient that satisfies the
// base "must address someone" check without tagging an agent, so it lets the
// sub-issue checks be exercised in isolation (TECH-3279 moved the base check
// ahead of them, so a bare issue ref no longer reaches the sub-issue checks).
const memberMention = "[@Other](mention://member/dddddddd-dddd-dddd-dddd-dddddddddddd)"

// TestSubIssueNoOwnerMention — check 1.
func TestSubIssueNoOwnerMention(t *testing.T) {
	g := New(allSubIssueFlags())
	ws := testWorkspaceID(t)

	// Sub-issue with owner mention → rejected.
	msg, ok := g.RejectComment(context.Background(), ws, "agent",
		agentMention+" "+ownerMention,
		true, []string{ownerUserID}, false)
	if ok {
		t.Fatalf("owner mention on sub-issue must be rejected")
	}
	if msg != OwnerMentionOnSubIssueMessage {
		t.Fatalf("wrong rejection message: %q", msg)
	}

	// Top-level issue with owner mention → passes (guard only applies to sub-issues).
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		agentMention+" "+ownerMention,
		false, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("owner mention on top-level issue must pass")
	}

	// Sub-issue mentioning non-owner member → passes check 1 (but check 2 may catch it).
	otherMember := "[@Other](mention://member/dddddddd-dddd-dddd-dddd-dddddddddddd)"
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		agentMention+" "+otherMember,
		true, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("non-owner member mention on sub-issue must pass check 1")
	}
}

// TestSubIssueRequireAgentTag — check 2.
func TestSubIssueRequireAgentTag(t *testing.T) {
	g := New(allSubIssueFlags())
	ws := testWorkspaceID(t)

	// Sub-issue with a recipient but no agent mention → rejected by check 2.
	msg, ok := g.RejectComment(context.Background(), ws, "agent",
		memberMention, // a recipient (passes base check) but no agent mention
		true, []string{ownerUserID}, false)
	if ok {
		t.Fatalf("sub-issue without agent tag must be rejected")
	}
	if msg != MissingAgentTagOnSubIssueMessage {
		t.Fatalf("wrong rejection message: %q", msg)
	}

	// Sub-issue with agent mention → passes.
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		agentMention+" "+issueRef,
		true, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("sub-issue with agent tag must pass check 2")
	}

	// Top-level issue with a recipient but no agent mention → passes (check 2
	// only applies to sub-issues).
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		memberMention,
		false, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("top-level issue without agent tag must pass")
	}
}

// TestSubIssueNoSplitSession — check 3.
func TestSubIssueNoSplitSession(t *testing.T) {
	g := New(allSubIssueFlags())
	ws := testWorkspaceID(t)

	// Sub-issue, task already posted on parent → rejected.
	msg, ok := g.RejectComment(context.Background(), ws, "agent",
		agentMention,
		true, []string{ownerUserID}, true /* taskPostedOnParent */)
	if ok {
		t.Fatalf("split-session on sub-issue must be rejected")
	}
	if msg != SplitSessionMessage {
		t.Fatalf("wrong rejection message: %q", msg)
	}

	// Sub-issue, task has NOT posted on parent → passes check 3.
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		agentMention,
		true, []string{ownerUserID}, false /* taskPostedOnParent */)
	if !ok {
		t.Fatalf("sub-issue with no prior parent post must pass check 3")
	}

	// Top-level issue, task posted on some issue → passes (check 3 only for sub-issues).
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		agentMention,
		false, nil, true /* taskPostedOnParent ignored for top-level */)
	if !ok {
		t.Fatalf("top-level issue must always pass check 3")
	}
}

// TestSubIssueChecksFlagOff — with individual sub-issue flags off, checks are
// skipped even when the base guard flag is on.
func TestSubIssueChecksFlagOff(t *testing.T) {
	ws := testWorkspaceID(t)

	// Only base guard on — sub-issue checks are all off.
	g := New(fakeFlags{flags: map[string]bool{FlagCommentTargetGuard: true}})

	// Owner mention on sub-issue → passes (no_owner_mention flag is off).
	_, ok := g.RejectComment(context.Background(), ws, "agent",
		ownerMention,
		true, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("owner mention must pass when sub-issue flag is off")
	}

	// Sub-issue with a recipient but no agent tag → passes (require_agent_tag
	// flag is off; base check is satisfied by the member mention).
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		memberMention,
		true, []string{ownerUserID}, false)
	if !ok {
		t.Fatalf("missing agent tag must pass when sub-issue flag is off")
	}

	// Split session on sub-issue → passes (no_split_session flag is off).
	_, ok = g.RejectComment(context.Background(), ws, "agent",
		ownerMention,
		true, []string{ownerUserID}, true)
	if !ok {
		t.Fatalf("split session must pass when sub-issue flag is off")
	}
}

// TestSubIssueTopLevelPassesUnchanged — top-level issues must never be affected
// by sub-issue checks regardless of flag state.
func TestSubIssueTopLevelPassesUnchanged(t *testing.T) {
	g := New(allSubIssueFlags())
	ws := testWorkspaceID(t)

	// Worst-case top-level comment: owner mention, no agent, split session.
	_, ok := g.RejectComment(context.Background(), ws, "agent",
		ownerMention, // has a target, so base check passes
		false, []string{ownerUserID}, true)
	if !ok {
		t.Fatalf("top-level issue must pass even with all sub-issue flags on")
	}
}

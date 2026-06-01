package commentguard

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// fakeFlags is a flagReader stub. enabled controls whether the workspace has
// the cerebro_comment_target_guard flag turned on; err forces a lookup error.
type fakeFlags struct {
	enabled bool
	err     error
}

func (f fakeFlags) ListCerebroWorkspaceFeatureFlags(_ context.Context, _ pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	if !f.enabled {
		return nil, nil
	}
	return []cerebrodb.ListCerebroWorkspaceFeatureFlagsRow{
		{FlagKey: FlagCommentTargetGuard, Enabled: true},
	}, nil
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
	g := New(fakeFlags{enabled: true})
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
		{"agent with issue link passes", "agent", "see [MUL-123](mention://issue/10fb2c2c-1ec4-449d-a081-b690ef70eb17)", true},
		{"agent with agent mention passes", "agent", "[@Tine](mention://agent/8bfccab3-89ea-40cc-b57d-c7eabaa30f50) please test", true},
		{"agent with squad mention passes", "agent", "[@Squad](mention://squad/8bfccab3-89ea-40cc-b57d-c7eabaa30f50)", true},
		{"agent with all mention passes", "agent", "[@all](mention://all/all)", true},
		{"agent text that merely looks like a mention is rejected", "agent", "talk to @Jesper soon", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := g.RejectComment(context.Background(), ws, tc.authorType, tc.content)
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
	g := New(fakeFlags{enabled: false})
	ws := testWorkspaceID(t)
	if msg, ok := g.RejectComment(context.Background(), ws, "agent", "no target here"); !ok || msg != "" {
		t.Fatalf("flag off must pass everything, got ok=%v msg=%q", ok, msg)
	}
}

// A nil Service, or one built with a nil reader, is a safe no-op.
func TestRejectCommentNilSafe(t *testing.T) {
	ws := testWorkspaceID(t)

	var nilService *Service
	if msg, ok := nilService.RejectComment(context.Background(), ws, "agent", "no target"); !ok || msg != "" {
		t.Fatalf("nil service must pass everything, got ok=%v msg=%q", ok, msg)
	}

	nilReader := New(nil)
	if msg, ok := nilReader.RejectComment(context.Background(), ws, "agent", "no target"); !ok || msg != "" {
		t.Fatalf("nil reader must pass everything, got ok=%v msg=%q", ok, msg)
	}
}

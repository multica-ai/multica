package channels

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeFlags is a createGuardFlagReader stub mirroring commentguard's test
// scaffold: enabled controls whether the workspace has the
// cerebro_channel_create_restricted flag turned on; err forces a lookup error.
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
		{FlagKey: FlagChannelCreateRestricted, Enabled: true},
	}, nil
}

// fakeMembers returns a configurable role for the lookup. err forces a DB error
// (treated as "deny" so the guard fails closed).
type fakeMembers struct {
	role string
	err  error
}

func (m fakeMembers) GetMemberByUserAndWorkspace(_ context.Context, _ db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	if m.err != nil {
		return db.Member{}, m.err
	}
	return db.Member{Role: m.role}, nil
}

func guardTestWorkspaceID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan("11bd8321-b6ac-4bee-ae41-6659a5064608"); err != nil {
		t.Fatalf("scan workspace id: %v", err)
	}
	return id
}

const callerUUID = "d7a6fa72-e68d-48ca-86be-2ab4313ecf44"

// With the flag OFF (the default — no override row), every call passes,
// regardless of caller type or role: the guard does nothing until an admin
// turns the flag on.
func TestRejectCreateFlagOffPassesEverything(t *testing.T) {
	g := NewCreateGuard(fakeFlags{enabled: false}, fakeMembers{role: "member"})
	ws := guardTestWorkspaceID(t)
	cases := []struct{ callerType, kind string }{
		{"member", "channel"},
		{"member", "dm"},
		{"agent", "channel"},
		{"agent", "dm"},
	}
	for _, tc := range cases {
		if msg, ok := g.RejectCreate(context.Background(), ws, tc.callerType, callerUUID, tc.kind); !ok || msg != "" {
			t.Fatalf("flag off must pass everything (%s/%s), got ok=%v msg=%q", tc.callerType, tc.kind, ok, msg)
		}
	}
}

// DMs are never gated, even when the flag is ON.
func TestRejectCreateDMAlwaysPasses(t *testing.T) {
	g := NewCreateGuard(fakeFlags{enabled: true}, fakeMembers{role: "member"})
	ws := guardTestWorkspaceID(t)
	if msg, ok := g.RejectCreate(context.Background(), ws, "member", callerUUID, "dm"); !ok || msg != "" {
		t.Fatalf("DM must pass when flag is on, got ok=%v msg=%q", ok, msg)
	}
	if msg, ok := g.RejectCreate(context.Background(), ws, "agent", callerUUID, "dm"); !ok || msg != "" {
		t.Fatalf("agent DM must pass when flag is on, got ok=%v msg=%q", ok, msg)
	}
}

// With the flag ON, owners and admins may create channels; members and agents
// cannot.
func TestRejectCreateFlagOnRoleMatrix(t *testing.T) {
	ws := guardTestWorkspaceID(t)
	cases := []struct {
		name       string
		callerType string
		role       string
		wantOK     bool
	}{
		{"owner can create", "member", "owner", true},
		{"admin can create", "member", "admin", true},
		{"plain member cannot create", "member", "member", false},
		{"viewer cannot create", "member", "viewer", false},
		{"agent cannot create (never owner/admin)", "agent", "n/a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewCreateGuard(fakeFlags{enabled: true}, fakeMembers{role: tc.role})
			msg, ok := g.RejectCreate(context.Background(), ws, tc.callerType, callerUUID, "channel")
			if ok != tc.wantOK {
				t.Fatalf("RejectCreate ok=%v, want %v", ok, tc.wantOK)
			}
			if !ok && msg == "" {
				t.Fatalf("rejected create must carry a message")
			}
			if ok && msg != "" {
				t.Fatalf("passing create must not carry a message, got %q", msg)
			}
		})
	}
}

// A DB error on the member lookup must fail closed (deny) rather than open up
// channel creation to anyone if the database hiccups.
func TestRejectCreateMemberLookupErrorFailsClosed(t *testing.T) {
	g := NewCreateGuard(fakeFlags{enabled: true}, fakeMembers{err: errors.New("boom")})
	ws := guardTestWorkspaceID(t)
	msg, ok := g.RejectCreate(context.Background(), ws, "member", callerUUID, "channel")
	if ok {
		t.Fatalf("member lookup error must deny, got ok=%v", ok)
	}
	if msg == "" {
		t.Fatalf("denial must carry a message")
	}
}

// A nil CreateGuard is a safe no-op so wiring never accidentally blocks.
func TestRejectCreateNilSafe(t *testing.T) {
	ws := guardTestWorkspaceID(t)
	var g *CreateGuard
	if msg, ok := g.RejectCreate(context.Background(), ws, "member", callerUUID, "channel"); !ok || msg != "" {
		t.Fatalf("nil guard must pass everything, got ok=%v msg=%q", ok, msg)
	}
}

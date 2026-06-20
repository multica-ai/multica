package toolpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// stateStore is a combined writer+resolver fake. It models two kinds of risk:
//
//   - baseDeny: a resource that resolves Deny ONLY while no workspace-root Allow
//     row exists. Pinning the floor clears it (Base=Deny / a workspace-layer rule
//     the pin overwrites).
//   - hardDeny: a resource denied at a layer ABOVE the workspace root. A
//     workspace Allow can never override it (most-restrictive-wins), so it stays
//     Blocked.
type stateStore struct {
	baseDeny       map[string]bool
	hardDeny       map[string]bool
	workspaceAllow map[string]bool // resources with a written workspace-root Allow row
	sets           int
	lastSet        SetParams
	writeErrOn     map[string]bool // force Set to fail for a resource (fail-closed test)
}

func (s *stateStore) Set(_ context.Context, p SetParams) (cerebrodb.UpsertCerebroToolPolicyRow, error) {
	if s.writeErrOn[p.ResourcePattern] {
		return cerebrodb.UpsertCerebroToolPolicyRow{}, errors.New("write boom")
	}
	s.sets++
	s.lastSet = p
	if p.Layer == LayerWorkspace && p.Setting == SettingAllow {
		if s.workspaceAllow == nil {
			s.workspaceAllow = map[string]bool{}
		}
		s.workspaceAllow[p.ResourcePattern] = true
	}
	return cerebrodb.UpsertCerebroToolPolicyRow{}, nil
}

func (s *stateStore) Resolve(_ context.Context, in Query) (Effective, error) {
	res := in.ResourcePattern
	if s.hardDeny[res] {
		return Effective{Setting: SettingDeny, Reason: "capped by agent"}, nil
	}
	if s.baseDeny[res] && !s.workspaceAllow[res] {
		return Effective{Setting: SettingDeny, Reason: "base deny"}, nil
	}
	return Effective{Setting: SettingAllow, Reason: "Allowed"}, nil
}

// Empty delta: nothing is at risk, so the migration writes nothing and is Ready.
func TestMigrate_EmptyDelta_NoWrites_Ready(t *testing.T) {
	s := &stateStore{}
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.sets != 0 {
		t.Fatalf("expected no writes on an empty delta, got %d", s.sets)
	}
	if !res.Ready() || len(res.Pinned) != 0 || len(res.Blocked) != 0 {
		t.Fatalf("expected clean Ready result, got %+v", res)
	}
}

// A base-deny risk is cleared by pinning a workspace-root Allow row: it ends up
// Pinned, not Blocked, and the migration is Ready.
func TestMigrate_BaseDeny_Cleared_Ready(t *testing.T) {
	s := &stateStore{baseDeny: map[string]bool{"github.com/firtal/b": true}}
	ws := pgUUID()
	g := grants()
	for i := range g {
		g[i].Query.WorkspaceID = ws
	}
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, g, pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.sets != 1 {
		t.Fatalf("expected exactly one Allow row written, got %d", s.sets)
	}
	if !res.Ready() {
		t.Fatalf("expected Ready after the only risk was cleared, got Blocked=%+v", res.Blocked)
	}
	if len(res.Pinned) != 1 || res.Pinned[0].Resource != "github.com/firtal/b" {
		t.Fatalf("expected the base-deny grant Pinned, got %+v", res.Pinned)
	}
	// The written row must be a workspace-root Allow keyed on the workspace.
	if s.lastSet.Layer != LayerWorkspace || s.lastSet.Setting != SettingAllow ||
		s.lastSet.ResourcePattern != "github.com/firtal/b" {
		t.Fatalf("written row is not a workspace-root Allow for the resource: %+v", s.lastSet)
	}
	if s.lastSet.SubjectID != ws || s.lastSet.WorkspaceID != ws {
		t.Fatalf("workspace layer must be keyed on the workspace id, got subject=%v ws=%v",
			s.lastSet.SubjectID, s.lastSet.WorkspaceID)
	}
}

// A hard deny above the workspace root cannot be cleared: the grant stays
// Blocked and the migration is NOT Ready (enablement must not proceed).
func TestMigrate_HardDeny_StaysBlocked_NotReady(t *testing.T) {
	s := &stateStore{hardDeny: map[string]bool{"cred-1": true}}
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready() {
		t.Fatal("expected NOT Ready while an intentional higher-layer Deny remains")
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Grant.Resource != "cred-1" {
		t.Fatalf("expected cred-1 Blocked, got %+v", res.Blocked)
	}
	if len(res.Pinned) != 0 {
		t.Fatalf("a hard-denied grant must not be reported Pinned, got %+v", res.Pinned)
	}
}

// Mixed: one clearable + one hard deny. The clearable is Pinned, the hard deny is
// Blocked, and the result is not Ready (Pinned and Blocked are disjoint).
func TestMigrate_Mixed_Partitioned_NotReady(t *testing.T) {
	s := &stateStore{
		baseDeny: map[string]bool{"github.com/firtal/a": true},
		hardDeny: map[string]bool{"cred-1": true},
	}
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready() {
		t.Fatal("expected NOT Ready while the hard deny remains")
	}
	if len(res.Pinned) != 1 || res.Pinned[0].Resource != "github.com/firtal/a" {
		t.Fatalf("expected the base-deny grant Pinned, got %+v", res.Pinned)
	}
	if len(res.Blocked) != 1 || res.Blocked[0].Grant.Resource != "cred-1" {
		t.Fatalf("expected cred-1 Blocked, got %+v", res.Blocked)
	}
}

// Idempotent: running the migration twice clears the same risk and ends Ready
// both times (the second run finds an empty delta and writes nothing).
func TestMigrate_Idempotent(t *testing.T) {
	s := &stateStore{baseDeny: map[string]bool{"github.com/firtal/b": true}}
	if _, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID()); err != nil {
		t.Fatalf("first run error: %v", err)
	}
	writesAfterFirst := s.sets
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID())
	if err != nil {
		t.Fatalf("second run error: %v", err)
	}
	if s.sets != writesAfterFirst {
		t.Fatalf("second run should write nothing (already 1:1), wrote %d more", s.sets-writesAfterFirst)
	}
	if !res.Ready() {
		t.Fatalf("expected Ready on the idempotent second run, got %+v", res)
	}
}

// A write error aborts and is returned — never swallowed into a false go-signal.
func TestMigrate_WriteError_FailsClosed(t *testing.T) {
	s := &stateStore{
		baseDeny:   map[string]bool{"github.com/firtal/b": true},
		writeErrOn: map[string]bool{"github.com/firtal/b": true},
	}
	_, err := MigrateAccessToAllowRows(context.Background(), s, s, grants(), pgUUID())
	if err == nil {
		t.Fatal("expected the write error to surface, got nil")
	}
}

// A nil writer cannot pin anything, so every at-risk grant is reported Blocked
// and the migration is not Ready.
func TestMigrate_NilWriter_FailsClosed(t *testing.T) {
	s := &stateStore{hardDeny: map[string]bool{"cred-1": true}}
	res, err := MigrateAccessToAllowRows(context.Background(), nil, s, grants(), pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready() {
		t.Fatal("expected NOT Ready with a nil writer and an existing risk")
	}
}

// A nil resolver cannot prove safety, so every grant is reported Blocked
// (fail-closed) rather than assumed safe.
func TestMigrate_NilResolver_FailsClosed(t *testing.T) {
	s := &stateStore{}
	res, err := MigrateAccessToAllowRows(context.Background(), s, nil, grants(), pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ready() {
		t.Fatal("expected NOT Ready with a nil resolver (cannot prove safety)")
	}
	if len(res.Blocked) != len(grants()) {
		t.Fatalf("expected every grant fail-closed Blocked, got %d of %d", len(res.Blocked), len(grants()))
	}
}

// No grants at all is trivially Ready.
func TestMigrate_NoGrants_Ready(t *testing.T) {
	s := &stateStore{}
	res, err := MigrateAccessToAllowRows(context.Background(), s, s, nil, pgUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Ready() || s.sets != 0 {
		t.Fatalf("empty grant set should be a clean no-op, got %+v sets=%d", res, s.sets)
	}
}

// pgUUID returns a valid non-zero UUID for the updatedBy arg.
func pgUUID() (u pgtype.UUID) {
	u.Bytes = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	u.Valid = true
	return u
}

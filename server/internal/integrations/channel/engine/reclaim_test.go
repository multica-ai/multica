package engine

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuid(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

type fakeReclaimQuerier struct {
	owner        *db.GetChannelInstallationReclaimByAppIDRow // nil => ErrNoRows (free)
	deleted      pgtype.UUID
	deleteCalled bool
}

func (f *fakeReclaimQuerier) GetChannelInstallationReclaimByAppID(_ context.Context, _ db.GetChannelInstallationReclaimByAppIDParams) (db.GetChannelInstallationReclaimByAppIDRow, error) {
	if f.owner == nil {
		return db.GetChannelInstallationReclaimByAppIDRow{}, pgx.ErrNoRows
	}
	return *f.owner, nil
}

func (f *fakeReclaimQuerier) DeleteChannelInstallation(_ context.Context, id pgtype.UUID) error {
	f.deleteCalled = true
	f.deleted = id
	return nil
}

func TestReclaimDeadAppID(t *testing.T) {
	const (
		ws    = "11111111-1111-1111-1111-111111111111"
		agent = "22222222-2222-2222-2222-222222222222"
		other = "88888888-8888-8888-8888-888888888888"
		ownID = "99999999-9999-9999-9999-999999999999"
	)
	ownerRow := func(ws, agent, status string, exists bool) *db.GetChannelInstallationReclaimByAppIDRow {
		return &db.GetChannelInstallationReclaimByAppIDRow{
			ID:          uuid(t, ownID),
			WorkspaceID: uuid(t, ws),
			AgentID:     uuid(t, agent),
			Status:      status,
			AgentExists: exists,
		}
	}

	tests := []struct {
		name        string
		owner       *db.GetChannelInstallationReclaimByAppIDRow
		wantErr     error
		wantDeleted bool
	}{
		{name: "free app_id (no owner)", owner: nil},
		{name: "same target reconnect", owner: ownerRow(ws, agent, "active", true)},
		{name: "dead: revoked same workspace", owner: ownerRow(ws, other, "revoked", true), wantDeleted: true},
		{name: "dead: orphaned agent gone", owner: ownerRow(other, other, "active", false), wantDeleted: true},
		// A revoked owner in ANOTHER workspace is recoverable data (Revoke preserves
		// the row for re-install), so it is refused as LIVE rather than hard-deleted.
		{name: "cross-workspace revoked refused", owner: ownerRow(other, other, "revoked", true), wantErr: ErrAppOwnedByLiveAgent},
		// An archived owner is indistinguishable from any other present, active
		// owner at the probe (agent_archived is no longer selected) and is refused
		// as LIVE — archiving is reversible, so we never silently reclaim it.
		{name: "live owner refused", owner: ownerRow(other, other, "active", true), wantErr: ErrAppOwnedByLiveAgent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeReclaimQuerier{owner: tc.owner}
			err := ReclaimDeadAppID(context.Background(), q, "dingtalk", "APP-X", uuid(t, ws), uuid(t, agent))
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if q.deleteCalled != tc.wantDeleted {
				t.Errorf("deleteCalled = %v, want %v", q.deleteCalled, tc.wantDeleted)
			}
			if tc.wantDeleted && q.deleted != uuid(t, ownID) {
				t.Errorf("deleted id = %v, want %v", q.deleted, ownID)
			}
		})
	}
}

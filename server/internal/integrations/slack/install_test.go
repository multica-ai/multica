package slack

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

type fakeInstallQueries struct {
	// existing, when set, is the agent's current row; UpsertChannelInstallation
	// returns it (an UPDATE) so a reconnect reuses the same row id.
	existing *db.ChannelInstallation
	// appIDTaken makes UpsertChannelInstallation report a unique-constraint
	// violation on the (channel_type, app_id) routing index — i.e. the pasted app
	// is already connected to a LIVE owner (the reclaim has run by then).
	appIDTaken   bool
	upsertParams db.UpsertChannelInstallationParams
	upsertCalled bool
	rowID        pgtype.UUID

	// reclaimedID, when set, is returned by ReclaimDeadChannelInstallationByAppID
	// to model a dead prior owner having been cleared; otherwise it reports
	// pgx.ErrNoRows (nothing was dead). reclaimCalled records that the install
	// path ran the reclaim before upserting.
	reclaimedID   *pgtype.UUID
	reclaimCalled bool
	// ownerWorkspaceID / ownerArchived / ownerMissing drive the live-owner lookup
	// that classifies an appIDTaken conflict into the right sentinel.
	ownerWorkspaceID pgtype.UUID
	ownerArchived    bool
	ownerMissing     bool
}

// WithTx returns the same fake — the fake tx is a no-op token.
func (f *fakeInstallQueries) WithTx(_ pgx.Tx) installQueries { return f }

func (f *fakeInstallQueries) ReclaimDeadChannelInstallationByAppID(_ context.Context, _ db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error) {
	f.reclaimCalled = true
	if f.reclaimedID != nil {
		return *f.reclaimedID, nil
	}
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (f *fakeInstallQueries) GetChannelInstallationOwnerByAppID(_ context.Context, _ db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error) {
	if f.ownerMissing {
		return db.GetChannelInstallationOwnerByAppIDRow{}, pgx.ErrNoRows
	}
	return db.GetChannelInstallationOwnerByAppIDRow{
		WorkspaceID:     f.ownerWorkspaceID,
		AgentArchivedAt: pgtype.Timestamptz{Valid: f.ownerArchived},
	}, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallation(_ context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	f.upsertCalled = true
	f.upsertParams = arg
	if f.appIDTaken {
		return db.ChannelInstallation{}, &pgconn.PgError{Code: "23505"}
	}
	id := f.rowID
	if f.existing != nil {
		id = f.existing.ID // reconnect updates the agent's existing row in place
	}
	return db.ChannelInstallation{
		ID:              id,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) ListChannelInstallationsByWorkspace(_ context.Context, _ db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return nil, nil
}

func (f *fakeInstallQueries) GetChannelInstallationInWorkspace(_ context.Context, _ db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, nil
}

func (f *fakeInstallQueries) SetChannelInstallationStatus(_ context.Context, _ db.SetChannelInstallationStatusParams) error {
	return nil
}

// fakeTx is a no-op pgx.Tx: embedding the interface satisfies it, and the
// install paths only ever call Commit / Rollback. committed records whether the
// install committed (the happy path) vs rolled back (a rejected install).
type fakeTx struct {
	pgx.Tx
	committed bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { return nil }

type fakeTxStarter struct{ tx *fakeTx }

func (f *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func newTestInstallService(t *testing.T, q installQueries) *InstallService {
	t.Helper()
	svc, err := newInstallService(q, &fakeTxStarter{tx: &fakeTx{}}, testBox(t), nil)
	if err != nil {
		t.Fatalf("newInstallService: %v", err)
	}
	return svc
}

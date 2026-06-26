package slack

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
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
	// existing, when set, is returned by GetChannelInstallationByAppID (else
	// pgx.ErrNoRows — a fresh install).
	existing     *db.ChannelInstallation
	upsertParams db.UpsertChannelInstallationByAppIDParams
	upsertCalled bool
	bindParams   db.CreateChannelUserBindingParams
	bindCalled   bool
	deleteCalled bool
	deleteParams db.DeleteChannelChatSessionBindingsByInstallationParams
	rowID        pgtype.UUID
}

// WithTx returns the same fake — the fake tx is a no-op token.
func (f *fakeInstallQueries) WithTx(_ pgx.Tx) installQueries { return f }

func (f *fakeInstallQueries) GetChannelInstallationByAppID(_ context.Context, _ db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	if f.existing == nil {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return *f.existing, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallationByAppID(_ context.Context, arg db.UpsertChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.upsertCalled = true
	f.upsertParams = arg
	// Simulate the query's `ON CONFLICT ... WHERE workspace_id = EXCLUDED.workspace_id`
	// guard: an app already owned by a different workspace updates no row.
	if f.existing != nil && f.existing.WorkspaceID != arg.WorkspaceID {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return db.ChannelInstallation{
		ID:              f.rowID,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) CreateChannelUserBinding(_ context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error) {
	f.bindCalled = true
	f.bindParams = arg
	return db.ChannelUserBinding{}, nil
}

func (f *fakeInstallQueries) DeleteChannelChatSessionBindingsByInstallation(_ context.Context, arg db.DeleteChannelChatSessionBindingsByInstallationParams) error {
	f.deleteCalled = true
	f.deleteParams = arg
	return nil
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

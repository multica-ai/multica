package dingtalk

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
	connector        *db.DingtalkConnector
	rowID            pgtype.UUID
	lockCalled       bool
	createCalled     bool
	updateCalled     bool
	grantCalled      bool
	createParams     db.CreateDingTalkConnectorParams
	updateParams     db.UpdateDingTalkConnectorCredentialsParams
	grantParams      []db.UpsertDingTalkWorkspaceGrantParams
	revokeParams     db.RevokeDingTalkWorkspaceGrantOnlyParams
	remainingActive  int64
	connectorRevoked bool
}

// WithTx returns the same fake — the fake tx is a no-op token.
func (f *fakeInstallQueries) WithTx(_ pgx.Tx) installQueries { return f }

func (f *fakeInstallQueries) LockWorkspaceForChatSessionCreate(_ context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return id, nil
}

func (f *fakeInstallQueries) LockDingTalkInstallTarget(_ context.Context, arg db.LockDingTalkInstallTargetParams) (pgtype.UUID, error) {
	return arg.WorkspaceID, nil
}

func (f *fakeInstallQueries) LockDingTalkConnectorAppID(_ context.Context, _ string) error {
	f.lockCalled = true
	return nil
}

func (f *fakeInstallQueries) GetDingTalkConnectorByAppIDForUpdate(_ context.Context, appID string) (db.DingtalkConnector, error) {
	if f.connector == nil || f.connector.AppID != appID {
		return db.DingtalkConnector{}, pgx.ErrNoRows
	}
	return *f.connector, nil
}

func (f *fakeInstallQueries) LockDingTalkConnectorForUpdate(_ context.Context, id pgtype.UUID) (db.DingtalkConnector, error) {
	if f.connector != nil {
		return *f.connector, nil
	}
	return db.DingtalkConnector{ID: id, Status: "active"}, nil
}

func (f *fakeInstallQueries) CreateDingTalkConnector(_ context.Context, arg db.CreateDingTalkConnectorParams) (db.DingtalkConnector, error) {
	f.createCalled = true
	f.createParams = arg
	row := db.DingtalkConnector{
		ID: f.rowID, AppID: arg.AppID, Config: arg.Config, Status: "active",
		InstallerUserID: arg.InstallerUserID,
	}
	f.connector = &row
	return row, nil
}

func (f *fakeInstallQueries) UpdateDingTalkConnectorCredentials(_ context.Context, arg db.UpdateDingTalkConnectorCredentialsParams) (db.DingtalkConnector, error) {
	f.updateCalled = true
	f.updateParams = arg
	f.connector.Config = arg.Config
	f.connector.Status = "active"
	f.connector.InstallerUserID = arg.InstallerUserID
	return *f.connector, nil
}

func (f *fakeInstallQueries) UpsertDingTalkWorkspaceGrant(_ context.Context, arg db.UpsertDingTalkWorkspaceGrantParams) (db.DingtalkWorkspaceGrant, error) {
	f.grantCalled = true
	f.grantParams = append(f.grantParams, arg)
	return db.DingtalkWorkspaceGrant{
		ID: f.rowID, ConnectorID: arg.ConnectorID, WorkspaceID: arg.WorkspaceID,
		DefaultAgentID: arg.DefaultAgentID, InstallerUserID: arg.InstallerUserID,
		Status: "active",
	}, nil
}

func (f *fakeInstallQueries) ListDingTalkInstallationsByWorkspace(_ context.Context, _ pgtype.UUID) ([]db.ListDingTalkInstallationsByWorkspaceRow, error) {
	return nil, nil
}

func (f *fakeInstallQueries) GetDingTalkInstallationInWorkspace(_ context.Context, _ db.GetDingTalkInstallationInWorkspaceParams) (db.GetDingTalkInstallationInWorkspaceRow, error) {
	return db.GetDingTalkInstallationInWorkspaceRow{}, nil
}

func (f *fakeInstallQueries) RevokeDingTalkWorkspaceGrantOnly(_ context.Context, arg db.RevokeDingTalkWorkspaceGrantOnlyParams) (pgtype.UUID, error) {
	f.revokeParams = arg
	return arg.ConnectorID, nil
}

func (f *fakeInstallQueries) CountActiveDingTalkWorkspaceGrants(_ context.Context, _ pgtype.UUID) (int64, error) {
	return f.remainingActive, nil
}

func (f *fakeInstallQueries) RevokeDingTalkConnector(_ context.Context, _ pgtype.UUID) error {
	f.connectorRevoked = true
	return nil
}

func (f *fakeInstallQueries) PurgeDingTalkConnectorUnscopedAudit(_ context.Context, _ pgtype.UUID) error {
	return nil
}

// fakeTx is a no-op pgx.Tx: embedding the interface satisfies it, and the
// install paths only ever call Commit / Rollback.
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

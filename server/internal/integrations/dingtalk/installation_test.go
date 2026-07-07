package dingtalk

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests cover the Upsert branching — fresh install, same-agent
// refresh, agent switch (the DingTalk device flow re-issues the SAME
// client_id on a re-scan, so switching agents must MOVE the row), and
// the conflict fences — through the installQueries seam, without a DB.

// fakeInstallQueries records calls and returns scripted rows/errors.
type fakeInstallQueries struct {
	prev    db.ChannelInstallation
	prevErr error

	upsertErr  error
	byAppIDErr error
	deleteErr  error

	upsertCalls  []db.UpsertChannelInstallationParams
	byAppIDCalls []db.UpsertChannelInstallationByAppIDParams
	deleteCalls  []db.DeleteChannelChatSessionBindingsByInstallationParams
}

func (f *fakeInstallQueries) WithTx(pgx.Tx) installQueries { return f }

func (f *fakeInstallQueries) GetChannelInstallationByAppID(context.Context, db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	if f.prevErr != nil {
		return db.ChannelInstallation{}, f.prevErr
	}
	return f.prev, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallation(_ context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	f.upsertCalls = append(f.upsertCalls, arg)
	if f.upsertErr != nil {
		return db.ChannelInstallation{}, f.upsertErr
	}
	return db.ChannelInstallation{
		ID:              uuidForInstallTest("99999999-9999-9999-9999-999999999999"),
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallationByAppID(_ context.Context, arg db.UpsertChannelInstallationByAppIDParams) (db.ChannelInstallation, error) {
	f.byAppIDCalls = append(f.byAppIDCalls, arg)
	if f.byAppIDErr != nil {
		return db.ChannelInstallation{}, f.byAppIDErr
	}
	// The conflict-update moves the EXISTING row: keep prev's id.
	return db.ChannelInstallation{
		ID:              f.prev.ID,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) DeleteChannelChatSessionBindingsByInstallation(_ context.Context, arg db.DeleteChannelChatSessionBindingsByInstallationParams) error {
	f.deleteCalls = append(f.deleteCalls, arg)
	return f.deleteErr
}

// fakeTx is a no-op pgx.Tx: embedding the interface satisfies it, and the
// only methods Upsert touches are Commit and Rollback.
type fakeTx struct {
	pgx.Tx
	committed bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { return nil }

type fakeTxStarter struct{ tx *fakeTx }

func (f *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func uuidForInstallTest(s string) pgtype.UUID {
	u, err := util.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

var (
	instTestWorkspace = uuidForInstallTest("11111111-1111-1111-1111-111111111111")
	instTestAgentA    = uuidForInstallTest("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	instTestAgentB    = uuidForInstallTest("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	instTestInstaller = uuidForInstallTest("77777777-7777-7777-7777-777777777777")
	instTestOtherWs   = uuidForInstallTest("22222222-2222-2222-2222-222222222222")
	instTestRowID     = uuidForInstallTest("33333333-3333-3333-3333-333333333333")
)

func newUpsertServiceForTest(t *testing.T, q *fakeInstallQueries) (*InstallationService, *fakeTx) {
	t.Helper()
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	tx := &fakeTx{}
	svc, err := newInstallationService(q, &fakeTxStarter{tx: tx}, nil, box)
	if err != nil {
		t.Fatalf("newInstallationService: %v", err)
	}
	return svc, tx
}

func installParamsForTest(agent pgtype.UUID) InstallationParams {
	return InstallationParams{
		WorkspaceID:     instTestWorkspace,
		AgentID:         agent,
		ClientID:        "ding-client-x",
		ClientSecret:    "s3cret",
		InstallerUserID: instTestInstaller,
	}
}

// prevRowForTest is the agent-A row holding client_id "ding-client-x".
func prevRowForTest(workspaceID pgtype.UUID) db.ChannelInstallation {
	return db.ChannelInstallation{
		ID:          instTestRowID,
		WorkspaceID: workspaceID,
		AgentID:     instTestAgentA,
		ChannelType: channelTypeDingTalk,
		Config:      []byte(`{"app_id":"ding-client-x"}`),
		Status:      "active",
	}
}

func TestUpsertFreshInstallUsesAgentKey(t *testing.T) {
	q := &fakeInstallQueries{prevErr: pgx.ErrNoRows}
	svc, tx := newUpsertServiceForTest(t, q)

	inst, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentA))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if len(q.upsertCalls) != 1 || len(q.byAppIDCalls) != 0 {
		t.Errorf("want 1 agent-keyed upsert and 0 app-keyed, got %d/%d", len(q.upsertCalls), len(q.byAppIDCalls))
	}
	if len(q.deleteCalls) != 0 {
		t.Errorf("fresh install must not retire chat sessions, got %d calls", len(q.deleteCalls))
	}
	if !tx.committed {
		t.Error("tx not committed")
	}
	if inst.ClientID != "ding-client-x" {
		t.Errorf("ClientID = %q, want ding-client-x", inst.ClientID)
	}
}

func TestUpsertSameAgentRefreshDoesNotRetireSessions(t *testing.T) {
	q := &fakeInstallQueries{prev: prevRowForTest(instTestWorkspace)}
	svc, tx := newUpsertServiceForTest(t, q)

	if _, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentA)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if len(q.upsertCalls) != 1 || len(q.byAppIDCalls) != 0 {
		t.Errorf("want agent-keyed refresh, got %d/%d upsert/byAppID calls", len(q.upsertCalls), len(q.byAppIDCalls))
	}
	if len(q.deleteCalls) != 0 {
		t.Errorf("same-agent refresh must not retire chat sessions, got %d calls", len(q.deleteCalls))
	}
	if !tx.committed {
		t.Error("tx not committed")
	}
}

func TestUpsertSwitchAgentMovesRowAndRetiresSessions(t *testing.T) {
	q := &fakeInstallQueries{prev: prevRowForTest(instTestWorkspace)}
	svc, tx := newUpsertServiceForTest(t, q)

	inst, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentB))
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if len(q.byAppIDCalls) != 1 || len(q.upsertCalls) != 0 {
		t.Fatalf("want 1 app-keyed upsert and 0 agent-keyed, got %d/%d", len(q.byAppIDCalls), len(q.upsertCalls))
	}
	if got := q.byAppIDCalls[0].AgentID; !uuidEqual(got, instTestAgentB) {
		t.Errorf("moved to agent %v, want agent B", got)
	}
	if len(q.deleteCalls) != 1 {
		t.Fatalf("want 1 chat-session retire, got %d", len(q.deleteCalls))
	}
	if !uuidEqual(q.deleteCalls[0].InstallationID, instTestRowID) {
		t.Errorf("retired bindings for %v, want the moved row %v", q.deleteCalls[0].InstallationID, instTestRowID)
	}
	if q.deleteCalls[0].ChannelType != channelTypeDingTalk {
		t.Errorf("retire channel_type = %q, want %q", q.deleteCalls[0].ChannelType, channelTypeDingTalk)
	}
	if !tx.committed {
		t.Error("tx not committed")
	}
	if !uuidEqual(inst.AgentID, instTestAgentB) {
		t.Errorf("returned AgentID = %v, want agent B", inst.AgentID)
	}
	if !uuidEqual(inst.ID, instTestRowID) {
		t.Errorf("returned ID = %v, want the moved row %v (user bindings must survive)", inst.ID, instTestRowID)
	}
}

func TestUpsertRefusesAppOwnedByAnotherWorkspace(t *testing.T) {
	q := &fakeInstallQueries{prev: prevRowForTest(instTestOtherWs)}
	svc, tx := newUpsertServiceForTest(t, q)

	_, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentB))
	if !errors.Is(err, ErrAppOwnedByAnotherWorkspace) {
		t.Fatalf("want ErrAppOwnedByAnotherWorkspace, got %v", err)
	}
	if len(q.upsertCalls)+len(q.byAppIDCalls)+len(q.deleteCalls) != 0 {
		t.Error("no writes may happen when the app belongs to another workspace")
	}
	if tx.committed {
		t.Error("tx must not commit on refusal")
	}
}

func TestUpsertSwitchAgentWorkspaceFenceRace(t *testing.T) {
	// The pre-check saw our workspace, but the fenced upsert updated zero
	// rows (another workspace claimed the app in between): same refusal.
	q := &fakeInstallQueries{prev: prevRowForTest(instTestWorkspace), byAppIDErr: pgx.ErrNoRows}
	svc, _ := newUpsertServiceForTest(t, q)

	_, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentB))
	if !errors.Is(err, ErrAppOwnedByAnotherWorkspace) {
		t.Fatalf("want ErrAppOwnedByAnotherWorkspace, got %v", err)
	}
	if len(q.deleteCalls) != 0 {
		t.Error("must not retire chat sessions when the move failed")
	}
}

func TestUpsertSwitchAgentTargetOccupied(t *testing.T) {
	// Agent B already holds a DIFFERENT dingtalk installation: the move
	// trips the (workspace_id, agent_id, channel_type) unique constraint.
	q := &fakeInstallQueries{
		prev:       prevRowForTest(instTestWorkspace),
		byAppIDErr: &pgconn.PgError{Code: pgUniqueViolation},
	}
	svc, _ := newUpsertServiceForTest(t, q)

	_, err := svc.Upsert(context.Background(), installParamsForTest(instTestAgentB))
	if !errors.Is(err, ErrAgentAlreadyConnected) {
		t.Fatalf("want ErrAgentAlreadyConnected, got %v", err)
	}
	if len(q.deleteCalls) != 0 {
		t.Error("must not retire chat sessions when the move failed")
	}
}

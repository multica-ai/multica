package wecom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file covers the install lifecycle contracts that can be verified
// without a live Postgres: begin replay/agent-mismatch/pending-resume/
// active-conflict/rate-limit, the maintenance-mode worker branch, and the
// worker's transition creating → pending → success against a fake WeCom
// provider. Every fake here is minimal on purpose — the real InstallStore
// interface is broad, but the wire contracts we need to verify only touch a
// small slice of it.

func mustParseUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

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

// fakeInstallStore is a minimal in-memory InstallStore for tests. Every
// column we care about is a map / slice; unused hooks return the empty row
// so we can leave them in place across the whole suite.
type fakeInstallStore struct {
	sessions       map[string]db.WecomInstallSession // keyed by UUID string
	byRequestHash  map[string]db.WecomInstallSession // keyed by hash|workspace|initiator
	pendingByAgent map[string]db.WecomInstallSession // keyed by workspace+agent
	activeByAgent  map[string]db.ChannelInstallation // keyed by workspace+agent

	windowTotal int64
	windowUser  int64

	upsertedInstallation db.ChannelInstallation
	upsertErr            error

	completedRows int64
	failedRows    int64

	reclaimCalled bool
}

func newFakeStore() *fakeInstallStore {
	return &fakeInstallStore{
		sessions:       map[string]db.WecomInstallSession{},
		byRequestHash:  map[string]db.WecomInstallSession{},
		pendingByAgent: map[string]db.WecomInstallSession{},
		activeByAgent:  map[string]db.ChannelInstallation{},
	}
}

func requestKey(ws pgtype.UUID, initiator pgtype.UUID, hash string) string {
	return util.UUIDToString(ws) + "|" + util.UUIDToString(initiator) + "|" + hash
}

func agentPendingKey(ws pgtype.UUID, agent pgtype.UUID) string {
	return util.UUIDToString(ws) + "|" + util.UUIDToString(agent)
}

func (f *fakeInstallStore) WithTx(pgx.Tx) InstallStore { return f }

func (f *fakeInstallStore) LockWecomInstallBeginWorkspace(context.Context, pgtype.UUID) error {
	return nil
}

func (f *fakeInstallStore) GetWecomInstallSessionByRequestHash(_ context.Context, arg db.GetWecomInstallSessionByRequestHashParams) (db.WecomInstallSession, error) {
	if row, ok := f.byRequestHash[requestKey(arg.WorkspaceID, arg.InitiatorUserID, arg.RequestKeyHash)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) GetPendingWecomInstallSessionByAgent(_ context.Context, arg db.GetPendingWecomInstallSessionByAgentParams) (db.WecomInstallSession, error) {
	if row, ok := f.pendingByAgent[agentPendingKey(arg.WorkspaceID, arg.AgentID)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) CountWecomInstallSessionsInWindow(context.Context, db.CountWecomInstallSessionsInWindowParams) (db.CountWecomInstallSessionsInWindowRow, error) {
	return db.CountWecomInstallSessionsInWindowRow{Total: f.windowTotal, ByUser: f.windowUser}, nil
}

func (f *fakeInstallStore) GetActiveWecomInstallationForAgent(_ context.Context, arg db.GetActiveWecomInstallationForAgentParams) (db.ChannelInstallation, error) {
	if row, ok := f.activeByAgent[agentPendingKey(arg.WorkspaceID, arg.AgentID)]; ok {
		return row, nil
	}
	return db.ChannelInstallation{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) CreateWecomInstallSession(_ context.Context, arg db.CreateWecomInstallSessionParams) (db.WecomInstallSession, error) {
	id, err := util.ParseUUID("11111111-1111-1111-1111-000000000001")
	if err != nil {
		return db.WecomInstallSession{}, err
	}
	row := db.WecomInstallSession{
		ID:              id,
		RequestKeyHash:  arg.RequestKeyHash,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		InitiatorUserID: arg.InitiatorUserID,
		Status:          InstallStatusCreating,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PollAfter:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	// Dedupe by request-hash so a repeat insert (not exercised by begin,
	// but harmless) does not double-record.
	f.sessions[util.UUIDToString(row.ID)] = row
	f.byRequestHash[requestKey(arg.WorkspaceID, arg.InitiatorUserID, arg.RequestKeyHash)] = row
	f.pendingByAgent[agentPendingKey(arg.WorkspaceID, arg.AgentID)] = row
	return row, nil
}

func (f *fakeInstallStore) GetWecomInstallSession(_ context.Context, id pgtype.UUID) (db.WecomInstallSession, error) {
	if row, ok := f.sessions[util.UUIDToString(id)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) ClaimDueWecomInstallSession(_ context.Context, arg db.ClaimDueWecomInstallSessionParams) (db.WecomInstallSession, error) {
	// Return the earliest not-terminal session whose lease is free. In
	// tests we usually only have one row.
	for k, row := range f.sessions {
		if row.Status != InstallStatusCreating && row.Status != InstallStatusPending {
			continue
		}
		if row.LeaseExpiresAt.Valid && row.LeaseExpiresAt.Time.After(time.Now()) && row.LeaseToken.Valid {
			continue
		}
		row.LeaseToken = arg.LeaseToken
		row.LeaseExpiresAt = arg.LeaseExpiresAt
		f.sessions[k] = row
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) DeferClaimedWecomInstallSession(_ context.Context, arg db.DeferClaimedWecomInstallSessionParams) (int64, error) {
	row, ok := f.sessions[util.UUIDToString(arg.ID)]
	if !ok || !row.LeaseToken.Valid || row.LeaseToken.String != arg.LeaseToken.String {
		return 0, nil
	}
	if row.Status != InstallStatusCreating && row.Status != InstallStatusPending {
		return 0, nil
	}
	row.LeaseToken = pgtype.Text{}
	row.LeaseExpiresAt = pgtype.Timestamptz{}
	row.PollAfter = arg.PollAfter
	row.Status = arg.Status
	if arg.ScodeEncrypted.Valid {
		row.ScodeEncrypted = arg.ScodeEncrypted
	}
	if arg.QrCodeUrlEncrypted.Valid {
		row.QrCodeUrlEncrypted = arg.QrCodeUrlEncrypted
	}
	if arg.ExpiresAt.Valid {
		row.ExpiresAt = arg.ExpiresAt
	}
	f.sessions[util.UUIDToString(arg.ID)] = row
	if row.Status == InstallStatusCreating || row.Status == InstallStatusPending {
		f.pendingByAgent[agentPendingKey(row.WorkspaceID, row.AgentID)] = row
	}
	return 1, nil
}

func (f *fakeInstallStore) CompleteWecomInstallSession(_ context.Context, arg db.CompleteWecomInstallSessionParams) (int64, error) {
	row, ok := f.sessions[util.UUIDToString(arg.ID)]
	if !ok || !row.LeaseToken.Valid || row.LeaseToken.String != arg.LeaseToken.String {
		return 0, nil
	}
	if row.Status != InstallStatusPending {
		return 0, nil
	}
	row.Status = InstallStatusSuccess
	row.InstallationID = arg.InstallationID
	row.ScodeEncrypted = pgtype.Text{}
	row.QrCodeUrlEncrypted = pgtype.Text{}
	row.LeaseToken = pgtype.Text{}
	row.LeaseExpiresAt = pgtype.Timestamptz{}
	f.sessions[util.UUIDToString(arg.ID)] = row
	delete(f.pendingByAgent, agentPendingKey(row.WorkspaceID, row.AgentID))
	f.completedRows++
	return 1, nil
}

func (f *fakeInstallStore) FailWecomInstallSession(_ context.Context, arg db.FailWecomInstallSessionParams) (int64, error) {
	row, ok := f.sessions[util.UUIDToString(arg.ID)]
	if !ok {
		return 0, nil
	}
	if row.Status != InstallStatusCreating && row.Status != InstallStatusPending {
		return 0, nil
	}
	row.Status = InstallStatusError
	row.ErrorReason = arg.ErrorReason
	row.ErrorMessage = arg.ErrorMessage
	row.ScodeEncrypted = pgtype.Text{}
	row.QrCodeUrlEncrypted = pgtype.Text{}
	row.LeaseToken = pgtype.Text{}
	row.LeaseExpiresAt = pgtype.Timestamptz{}
	f.sessions[util.UUIDToString(arg.ID)] = row
	delete(f.pendingByAgent, agentPendingKey(row.WorkspaceID, row.AgentID))
	f.failedRows++
	return 1, nil
}

func (f *fakeInstallStore) PurgeTerminalWecomInstallSessions(context.Context, db.PurgeTerminalWecomInstallSessionsParams) (int64, error) {
	return 0, nil
}

func (f *fakeInstallStore) GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error) {
	return db.Agent{}, nil
}

func (f *fakeInstallStore) GetUser(context.Context, pgtype.UUID) (db.User, error) {
	return db.User{Language: pgtype.Text{String: "zh-CN", Valid: true}}, nil
}

func (f *fakeInstallStore) ReclaimDeadChannelInstallationByAppID(context.Context, db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error) {
	f.reclaimCalled = true
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) DeleteRevokedChannelInstallationForReplacement(context.Context, db.DeleteRevokedChannelInstallationForReplacementParams) (pgtype.UUID, error) {
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) UpsertChannelInstallation(_ context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	if f.upsertErr != nil {
		return db.ChannelInstallation{}, f.upsertErr
	}
	id, err := util.ParseUUID("22222222-2222-2222-2222-000000000001")
	if err != nil {
		return db.ChannelInstallation{}, err
	}
	f.upsertedInstallation = db.ChannelInstallation{
		ID:              id,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		Status:          "active",
		InstallerUserID: arg.InstallerUserID,
	}
	return f.upsertedInstallation, nil
}

func (f *fakeInstallStore) GetChannelInstallationInWorkspace(context.Context, db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) SetChannelInstallationStatus(context.Context, db.SetChannelInstallationStatusParams) error {
	return nil
}

func (f *fakeInstallStore) ListChannelInstallationsByWorkspace(context.Context, db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return nil, nil
}

// fakeTx is a no-op pgx.Tx: install code only calls Commit / Rollback.
type fakeTx struct {
	pgx.Tx
	committed bool
	rolled    bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { t.rolled = true; return nil }

type fakeTxStarter struct{ last *fakeTx }

func (f *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) {
	f.last = &fakeTx{}
	return f.last, nil
}

type fakeProvider struct {
	generateResult GenerateResult
	generateErr    error
	generateCalls  int

	queryResult QueryResult
	queryErr    error
	queryCalls  int
}

func (p *fakeProvider) Generate(context.Context) (GenerateResult, error) {
	p.generateCalls++
	return p.generateResult, p.generateErr
}

func (p *fakeProvider) QueryResult(_ context.Context, scode string) (QueryResult, error) {
	p.queryCalls++
	_ = scode
	return p.queryResult, p.queryErr
}

func testInstallService(t *testing.T, store *fakeInstallStore, provider Provider, box *secretbox.Box) (*InstallService, *fakeTxStarter, func()) {
	t.Helper()
	txSt := &fakeTxStarter{}
	notified := 0
	notify := func() { notified++ }
	svc := newInstallService(store, txSt, InstallServiceConfig{
		SourceID:            "src",
		Box:                 box,
		Provider:            provider,
		RatePerUser:         5,
		RatePerWorkspace:    30,
		RateWindow:          10 * time.Minute,
		QRTTL:               5 * time.Minute,
		GenerateDeadline:    30 * time.Second,
		PendingPollInterval: 2 * time.Second,
		LeaseTTL:            30 * time.Second,
		UpstreamTimeout:     time.Second,
	}, notify)
	return svc, txSt, func() { _ = notified }
}

const (
	testWS       = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	testAgent1   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	testAgent2   = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	testUser1    = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	testUser2    = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	testIdemKey  = "idem-key-abc-123"
	testIdemKey2 = "idem-key-def-456"
)

func TestBeginInstall_HappyPath(t *testing.T) {
	store := newFakeStore()
	svc, tx, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	res, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	})
	if err != nil {
		t.Fatalf("BeginInstall: %v", err)
	}
	if res.SessionID == "" {
		t.Fatalf("expected a session id")
	}
	if res.Status != InstallStatusCreating {
		t.Fatalf("expected status=creating, got %q", res.Status)
	}
	if tx.last == nil || !tx.last.committed {
		t.Fatalf("expected the tx to be committed")
	}
}

func TestBeginInstall_ReplaySameKeyReturnsSameSession(t *testing.T) {
	store := newFakeStore()
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	params := BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	}
	first, err := svc.BeginInstall(context.Background(), params)
	if err != nil {
		t.Fatalf("first begin: %v", err)
	}
	second, err := svc.BeginInstall(context.Background(), params)
	if err != nil {
		t.Fatalf("replay begin: %v", err)
	}
	if first.SessionID != second.SessionID {
		t.Fatalf("expected replay to return the same session id: %q vs %q",
			first.SessionID, second.SessionID)
	}
}

func TestBeginInstall_AgentMismatchOnReplay(t *testing.T) {
	store := newFakeStore()
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	if _, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	}); err != nil {
		t.Fatalf("first begin: %v", err)
	}
	_, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent2),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	})
	if !errors.Is(err, ErrAgentMismatch) {
		t.Fatalf("expected ErrAgentMismatch, got %v", err)
	}
}

func TestBeginInstall_ActiveInstallationConflict(t *testing.T) {
	store := newFakeStore()
	store.activeByAgent[agentPendingKey(mustParseUUID(t, testWS), mustParseUUID(t, testAgent1))] = db.ChannelInstallation{
		ID: mustParseUUID(t, "99999999-9999-9999-9999-999999999999"),
	}
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	_, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	})
	if !errors.Is(err, ErrActiveInstallationExists) {
		t.Fatalf("expected ErrActiveInstallationExists, got %v", err)
	}
}

func TestBeginInstall_PendingResumeSameInitiator(t *testing.T) {
	store := newFakeStore()
	// Pre-populate a pending session for the same (ws, agent).
	existingID := mustParseUUID(t, "55555555-5555-5555-5555-555555555555")
	existing := db.WecomInstallSession{
		ID:              existingID,
		WorkspaceID:     mustParseUUID(t, testWS),
		AgentID:         mustParseUUID(t, testAgent1),
		InitiatorUserID: mustParseUUID(t, testUser1),
		Status:          InstallStatusPending,
	}
	store.sessions[util.UUIDToString(existingID)] = existing
	store.pendingByAgent[agentPendingKey(existing.WorkspaceID, existing.AgentID)] = existing

	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	res, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey2, // different key — pending recovery, not replay
	})
	if err != nil {
		t.Fatalf("resume begin: %v", err)
	}
	if res.SessionID != util.UUIDToString(existingID) {
		t.Fatalf("expected to resume existing session, got %q", res.SessionID)
	}
	if res.Status != InstallStatusPending {
		t.Fatalf("expected status=pending, got %q", res.Status)
	}
}

func TestBeginInstall_PendingConflictOtherInitiator(t *testing.T) {
	store := newFakeStore()
	existingID := mustParseUUID(t, "55555555-5555-5555-5555-555555555555")
	existing := db.WecomInstallSession{
		ID:              existingID,
		WorkspaceID:     mustParseUUID(t, testWS),
		AgentID:         mustParseUUID(t, testAgent1),
		InitiatorUserID: mustParseUUID(t, testUser1),
		Status:          InstallStatusPending,
	}
	store.sessions[util.UUIDToString(existingID)] = existing
	store.pendingByAgent[agentPendingKey(existing.WorkspaceID, existing.AgentID)] = existing

	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	_, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser2), // different, non-admin
		IdempotencyKey: testIdemKey2,
	})
	if !errors.Is(err, ErrInstallInProgress) {
		t.Fatalf("expected ErrInstallInProgress, got %v", err)
	}
}

func TestBeginInstall_RateLimit(t *testing.T) {
	store := newFakeStore()
	store.windowUser = 5 // caller at the per-user cap already
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	_, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID:    mustParseUUID(t, testWS),
		AgentID:        mustParseUUID(t, testAgent1),
		InitiatorID:    mustParseUUID(t, testUser1),
		IdempotencyKey: testIdemKey,
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestBeginInstall_RequiresIdempotencyKey(t *testing.T) {
	store := newFakeStore()
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, testBox(t))
	_, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID: mustParseUUID(t, testWS),
		AgentID:     mustParseUUID(t, testAgent1),
		InitiatorID: mustParseUUID(t, testUser1),
	})
	if !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("expected ErrIdempotencyKeyRequired, got %v", err)
	}
}

func TestGetSession_DecryptQRCodeURL(t *testing.T) {
	store := newFakeStore()
	box := testBox(t)
	svc, _, _ := testInstallService(t, store, &fakeProvider{}, box)

	// Seed a pending session with an encrypted URL.
	sealed, err := sealAndEncode(box, []byte("https://work.weixin.qq.com/qr"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	id := mustParseUUID(t, "66666666-6666-6666-6666-666666666666")
	row := db.WecomInstallSession{
		ID:                 id,
		WorkspaceID:        mustParseUUID(t, testWS),
		AgentID:            mustParseUUID(t, testAgent1),
		InitiatorUserID:    mustParseUUID(t, testUser1),
		Status:             InstallStatusPending,
		QrCodeUrlEncrypted: pgtype.Text{String: sealed, Valid: true},
		ExpiresAt:          pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
	}
	store.sessions[util.UUIDToString(id)] = row

	// Authorized viewer gets the URL.
	snap, err := svc.GetSession(context.Background(), row.WorkspaceID, id, true)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if snap.QRCodeURL != "https://work.weixin.qq.com/qr" {
		t.Fatalf("expected decrypted URL, got %q", snap.QRCodeURL)
	}

	// Non-authorized viewer never gets the URL.
	snap, err = svc.GetSession(context.Background(), row.WorkspaceID, id, false)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if snap.QRCodeURL != "" {
		t.Fatalf("expected empty URL, got %q", snap.QRCodeURL)
	}

	// Cross-workspace lookup is a hard 404.
	if _, err := svc.GetSession(context.Background(), mustParseUUID(t, testAgent2), id, true); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound for cross-workspace, got %v", err)
	}
}

func TestInstallWorker_MaintenanceModeFailsSession(t *testing.T) {
	// Service with no provider / no source id → not Configured, so the
	// worker must terminate creating sessions with
	// error/integration_unconfigured (spec §7.1.1).
	store := newFakeStore()
	txSt := &fakeTxStarter{}
	svc := newInstallService(store, txSt, InstallServiceConfig{
		Box:              testBox(t), // key present, but no provider
		SourceID:         "",
		Provider:         nil,
		RatePerUser:      5,
		RatePerWorkspace: 30,
		LeaseTTL:         30 * time.Second,
	}, nil)
	// Seed a creating row directly (bypassing begin, which requires a
	// configured service).
	id := mustParseUUID(t, "77777777-7777-7777-7777-777777777777")
	row := db.WecomInstallSession{
		ID:              id,
		WorkspaceID:     mustParseUUID(t, testWS),
		AgentID:         mustParseUUID(t, testAgent1),
		InitiatorUserID: mustParseUUID(t, testUser1),
		Status:          InstallStatusCreating,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PollAfter:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	store.sessions[util.UUIDToString(id)] = row

	m := newRecordingMetrics()
	worker := NewInstallWorker(svc, InstallWorkerConfig{PollInterval: time.Second, PurgeInterval: time.Minute, Metrics: m})
	worked, err := worker.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if !worked {
		t.Fatalf("expected the worker to claim the row")
	}
	updated := store.sessions[util.UUIDToString(id)]
	if updated.Status != InstallStatusError {
		t.Fatalf("expected status=error, got %q", updated.Status)
	}
	if updated.ErrorReason.String != InstallErrorIntegrationUnconfigured {
		t.Fatalf("expected reason=integration_unconfigured, got %q", updated.ErrorReason.String)
	}
	// The failure reason must also be reported, otherwise a deployment that
	// silently loses its provider config looks identical to an idle one.
	if got := m.installTerminals(InstallErrorIntegrationUnconfigured); got != 1 {
		t.Fatalf("install terminals for %q = %d, want 1", InstallErrorIntegrationUnconfigured, got)
	}
}

func TestInstallWorker_HappyPathCreatingToSuccess(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{
		generateResult: GenerateResult{Scode: "scode-1", AuthURL: "https://qr"},
		queryResult:    QueryResult{Status: QueryStatusSuccess, BotInfo: &BotInfo{BotID: "bot-1", Secret: "sec-1"}},
	}
	svc, _, _ := testInstallService(t, store, provider, testBox(t))
	// Seed a creating row.
	id := mustParseUUID(t, "88888888-8888-8888-8888-888888888888")
	row := db.WecomInstallSession{
		ID:              id,
		WorkspaceID:     mustParseUUID(t, testWS),
		AgentID:         mustParseUUID(t, testAgent1),
		InitiatorUserID: mustParseUUID(t, testUser1),
		Status:          InstallStatusCreating,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PollAfter:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	store.sessions[util.UUIDToString(id)] = row

	m := newRecordingMetrics()
	worker := NewInstallWorker(svc, InstallWorkerConfig{Metrics: m})
	// First pass: creating → pending (generate succeeds).
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if got := store.sessions[util.UUIDToString(id)].Status; got != InstallStatusPending {
		t.Fatalf("expected status=pending after generate, got %q", got)
	}
	// Reset poll_after so the next claim can pick the row up in this
	// synchronous test (fake DB does not obey the wall clock).
	updated := store.sessions[util.UUIDToString(id)]
	updated.PollAfter = pgtype.Timestamptz{Time: time.Now().Add(-time.Second), Valid: true}
	store.sessions[util.UUIDToString(id)] = updated
	// Second pass: pending → success (query_result success).
	if _, err := worker.processOne(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	final := store.sessions[util.UUIDToString(id)]
	if final.Status != InstallStatusSuccess {
		t.Fatalf("expected status=success, got %q (err=%q)", final.Status, final.ErrorMessage.String)
	}
	if !final.InstallationID.Valid {
		t.Fatalf("expected installation_id set on success")
	}
	if !store.reclaimCalled {
		t.Fatalf("expected reclaim to be called before upsert")
	}
	if provider.generateCalls != 1 || provider.queryCalls != 1 {
		t.Fatalf("unexpected provider calls: generate=%d query=%d",
			provider.generateCalls, provider.queryCalls)
	}
	// Success is reported too, so the failure counters have a denominator.
	if got := m.installTerminals(installResultSucceeded); got != 1 {
		t.Fatalf("install terminals for %q = %d, want 1", installResultSucceeded, got)
	}
}

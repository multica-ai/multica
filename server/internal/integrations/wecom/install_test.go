package wecom

// install_test.go — the scan-install contracts that hold without a live
// Postgres: which begins are admitted, which are refused and why, and the
// worker's creating → pending → success walk against a fake WeCom provider.
//
// The fakes are deliberately minimal. InstallStore is a broad interface, but the
// decisions worth pinning touch a small slice of it.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

// installUUID returns a distinct, stable UUID per n. It does not take a
// *testing.T because the fake store also needs it, outside any test scope.
func installUUID(n int) pgtype.UUID {
	u, err := util.ParseUUID(strings.Repeat("0", 7) + string(rune('0'+n)) + "-1111-1111-1111-111111111111")
	if err != nil {
		panic("install_test: bad fixture uuid: " + err.Error())
	}
	return u
}

// fakeInstallStore is an in-memory InstallStore.
type fakeInstallStore struct {
	sessions       map[string]db.WecomInstallSession
	byRequestHash  map[string]db.WecomInstallSession
	pendingByAgent map[string]db.WecomInstallSession
	activeByAgent  map[string]db.ChannelInstallation

	windowTotal int64
	windowUser  int64

	claimQueue []db.WecomInstallSession

	created   []db.CreateWecomInstallSessionParams
	deferred  []db.DeferClaimedWecomInstallSessionParams
	completed []db.CompleteWecomInstallSessionParams
	failed    []db.FailWecomInstallSessionParams

	completeRows int64
	lockErr      error
}

func newFakeInstallStore() *fakeInstallStore {
	return &fakeInstallStore{
		sessions:       map[string]db.WecomInstallSession{},
		byRequestHash:  map[string]db.WecomInstallSession{},
		pendingByAgent: map[string]db.WecomInstallSession{},
		activeByAgent:  map[string]db.ChannelInstallation{},
		completeRows:   1,
	}
}

func requestKey(ws, initiator pgtype.UUID, hash string) string {
	return util.UUIDToString(ws) + "|" + util.UUIDToString(initiator) + "|" + hash
}

func agentKey(ws, agent pgtype.UUID) string {
	return util.UUIDToString(ws) + "|" + util.UUIDToString(agent)
}

func (f *fakeInstallStore) WithTx(pgx.Tx) InstallStore { return f }

func (f *fakeInstallStore) LockWecomInstallBeginWorkspace(context.Context, pgtype.UUID) error {
	return f.lockErr
}

func (f *fakeInstallStore) GetWecomInstallSessionByRequestHash(_ context.Context, arg db.GetWecomInstallSessionByRequestHashParams) (db.WecomInstallSession, error) {
	if row, ok := f.byRequestHash[requestKey(arg.WorkspaceID, arg.InitiatorUserID, arg.RequestKeyHash)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) GetPendingWecomInstallSessionByAgent(_ context.Context, arg db.GetPendingWecomInstallSessionByAgentParams) (db.WecomInstallSession, error) {
	if row, ok := f.pendingByAgent[agentKey(arg.WorkspaceID, arg.AgentID)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) CountWecomInstallSessionsInWindow(context.Context, db.CountWecomInstallSessionsInWindowParams) (db.CountWecomInstallSessionsInWindowRow, error) {
	return db.CountWecomInstallSessionsInWindowRow{Total: f.windowTotal, ByUser: f.windowUser}, nil
}

func (f *fakeInstallStore) GetActiveWecomInstallationForAgent(_ context.Context, arg db.GetActiveWecomInstallationForAgentParams) (db.ChannelInstallation, error) {
	if row, ok := f.activeByAgent[agentKey(arg.WorkspaceID, arg.AgentID)]; ok {
		return row, nil
	}
	return db.ChannelInstallation{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) CreateWecomInstallSession(_ context.Context, arg db.CreateWecomInstallSessionParams) (db.WecomInstallSession, error) {
	f.created = append(f.created, arg)
	row := db.WecomInstallSession{
		ID:              installUUID(9),
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		InitiatorUserID: arg.InitiatorUserID,
		RequestKeyHash:  arg.RequestKeyHash,
		Status:          InstallStatusCreating,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	f.sessions[util.UUIDToString(row.ID)] = row
	return row, nil
}

func (f *fakeInstallStore) GetWecomInstallSession(_ context.Context, id pgtype.UUID) (db.WecomInstallSession, error) {
	if row, ok := f.sessions[util.UUIDToString(id)]; ok {
		return row, nil
	}
	return db.WecomInstallSession{}, pgx.ErrNoRows
}

func (f *fakeInstallStore) ClaimDueWecomInstallSession(context.Context, db.ClaimDueWecomInstallSessionParams) (db.WecomInstallSession, error) {
	if len(f.claimQueue) == 0 {
		return db.WecomInstallSession{}, pgx.ErrNoRows
	}
	row := f.claimQueue[0]
	f.claimQueue = f.claimQueue[1:]
	return row, nil
}

func (f *fakeInstallStore) DeferClaimedWecomInstallSession(_ context.Context, arg db.DeferClaimedWecomInstallSessionParams) (int64, error) {
	f.deferred = append(f.deferred, arg)
	return 1, nil
}

func (f *fakeInstallStore) CompleteWecomInstallSession(_ context.Context, arg db.CompleteWecomInstallSessionParams) (int64, error) {
	f.completed = append(f.completed, arg)
	return f.completeRows, nil
}

func (f *fakeInstallStore) FailWecomInstallSession(_ context.Context, arg db.FailWecomInstallSessionParams) (int64, error) {
	f.failed = append(f.failed, arg)
	return 1, nil
}

func (f *fakeInstallStore) PurgeTerminalWecomInstallSessions(context.Context, db.PurgeTerminalWecomInstallSessionsParams) (int64, error) {
	return 0, nil
}

// noopTx satisfies pgx.Tx for the begin transaction; the fake store ignores it.
type noopTx struct{ pgx.Tx }

func (noopTx) Commit(context.Context) error   { return nil }
func (noopTx) Rollback(context.Context) error { return nil }

type noopTxStarter struct{}

func (noopTxStarter) Begin(context.Context) (pgx.Tx, error) { return noopTx{}, nil }

// fakeProvider scripts the WeCom QR endpoints.
type fakeProvider struct {
	gen       GenerateResult
	genErr    error
	genCalls  int
	query     QueryResult
	queryErr  error
	queryCall int
}

func (p *fakeProvider) Generate(context.Context) (GenerateResult, error) {
	p.genCalls++
	return p.gen, p.genErr
}

func (p *fakeProvider) QueryResult(context.Context, string) (QueryResult, error) {
	p.queryCall++
	return p.query, p.queryErr
}

// fakeBotBinder records the bind and can fail it.
type fakeBotBinder struct {
	calls []InstallationParams
	inst  Installation
	err   error
}

func (b *fakeBotBinder) Upsert(_ context.Context, p InstallationParams) (Installation, error) {
	b.calls = append(b.calls, p)
	if b.err != nil {
		return Installation{}, b.err
	}
	return b.inst, nil
}

func newTestInstallService(t *testing.T, store InstallStore, provider Provider, binder BotBinder) *InstallService {
	t.Helper()
	return newInstallService(store, noopTxStarter{}, binder, InstallServiceConfig{
		Provider: provider,
		Box:      testBox(t),
	}, nil)
}

func beginParams(t *testing.T, key string) BeginInstallParams {
	t.Helper()
	return BeginInstallParams{
		WorkspaceID:    installUUID(1),
		AgentID:        installUUID(2),
		InitiatorID:    installUUID(3),
		IdempotencyKey: key,
	}
}

func TestBeginInstall_CreatesASession(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})

	res, err := svc.BeginInstall(context.Background(), beginParams(t, "key-1"))
	if err != nil {
		t.Fatalf("BeginInstall: %v", err)
	}
	if res.SessionID == "" || res.Status != InstallStatusCreating {
		t.Errorf("result = %+v, want a session id in creating", res)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(store.created))
	}
	// The raw client key must never be stored; only its hash.
	if store.created[0].RequestKeyHash == "key-1" {
		t.Error("the raw idempotency key was persisted")
	}
	if store.created[0].RequestKeyHash != hashIdempotencyKey("key-1") {
		t.Error("request_key_hash is not the hash of the client key")
	}
}

// A replayed request with the same key must return the SAME session, not spend a
// second WeCom generate slot and show the admin a second QR.
func TestBeginInstall_ReplayReturnsTheSameSession(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
	p := beginParams(t, "key-1")

	existing := db.WecomInstallSession{
		ID:              installUUID(7),
		WorkspaceID:     p.WorkspaceID,
		AgentID:         p.AgentID,
		InitiatorUserID: p.InitiatorID,
		Status:          InstallStatusPending,
	}
	store.byRequestHash[requestKey(p.WorkspaceID, p.InitiatorID, hashIdempotencyKey("key-1"))] = existing

	res, err := svc.BeginInstall(context.Background(), p)
	if err != nil {
		t.Fatalf("BeginInstall: %v", err)
	}
	if res.SessionID != util.UUIDToString(existing.ID) {
		t.Errorf("session id = %q, want the existing %q", res.SessionID, util.UUIDToString(existing.ID))
	}
	if len(store.created) != 0 {
		t.Error("a replay must not create a second session")
	}
}

// The same key pointed at a different agent is a hard conflict: an HTTP retry
// must not silently move which agent gets the bot.
func TestBeginInstall_ReplayAcrossAgentsIsAConflict(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
	p := beginParams(t, "key-1")

	store.byRequestHash[requestKey(p.WorkspaceID, p.InitiatorID, hashIdempotencyKey("key-1"))] = db.WecomInstallSession{
		ID:              installUUID(7),
		WorkspaceID:     p.WorkspaceID,
		AgentID:         installUUID(8), // a different agent
		InitiatorUserID: p.InitiatorID,
		Status:          InstallStatusPending,
	}

	if _, err := svc.BeginInstall(context.Background(), p); !errors.Is(err, ErrAgentMismatch) {
		t.Errorf("err = %v, want ErrAgentMismatch", err)
	}
}

func TestBeginInstall_ActiveInstallationIsAConflict(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
	p := beginParams(t, "key-1")
	store.activeByAgent[agentKey(p.WorkspaceID, p.AgentID)] = db.ChannelInstallation{Status: "active"}

	if _, err := svc.BeginInstall(context.Background(), p); !errors.Is(err, ErrActiveInstallationExists) {
		t.Errorf("err = %v, want ErrActiveInstallationExists", err)
	}
	if len(store.created) != 0 {
		t.Error("a conflicting begin must not create a session")
	}
}

func TestBeginInstall_PendingSessionResumeRules(t *testing.T) {
	t.Parallel()
	pendingID := installUUID(7)

	cases := []struct {
		name       string
		initiator  pgtype.UUID
		isAdmin    bool
		wantResume bool
	}{
		// The person who started it can always pick it back up.
		{"same initiator resumes", installUUID(3), false, true},
		// An admin can rescue a colleague's stuck session rather than leaving the
		// agent's install slot locked until it expires.
		{"admin resumes someone else's", installUUID(4), true, true},
		// Anyone else is told it is in progress.
		{"other member is refused", installUUID(4), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeInstallStore()
			svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
			p := beginParams(t, "key-1")
			p.InitiatorID = tc.initiator
			p.CallerIsWorkspaceAdmin = tc.isAdmin

			store.pendingByAgent[agentKey(p.WorkspaceID, p.AgentID)] = db.WecomInstallSession{
				ID:              pendingID,
				WorkspaceID:     p.WorkspaceID,
				AgentID:         p.AgentID,
				InitiatorUserID: installUUID(3),
				Status:          InstallStatusPending,
			}

			res, err := svc.BeginInstall(context.Background(), p)
			if !tc.wantResume {
				if !errors.Is(err, ErrInstallInProgress) {
					t.Fatalf("err = %v, want ErrInstallInProgress", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BeginInstall: %v", err)
			}
			if res.SessionID != util.UUIDToString(pendingID) {
				t.Errorf("session id = %q, want the pending session", res.SessionID)
			}
			if len(store.created) != 0 {
				t.Error("a resume must not create a second session")
			}
		})
	}
}

func TestBeginInstall_RateLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		total, user int64
	}{
		{"per-user cap", 0, defaultRatePerUser},
		{"per-workspace cap", defaultRatePerWorkspace, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeInstallStore()
			store.windowTotal = tc.total
			store.windowUser = tc.user
			svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})

			if _, err := svc.BeginInstall(context.Background(), beginParams(t, "key-1")); !errors.Is(err, ErrRateLimited) {
				t.Errorf("err = %v, want ErrRateLimited", err)
			}
		})
	}
}

func TestBeginInstall_RejectsBadIdempotencyKeys(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})

	if _, err := svc.BeginInstall(context.Background(), beginParams(t, "   ")); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Errorf("blank key err = %v, want ErrIdempotencyKeyRequired", err)
	}
	long := strings.Repeat("k", maxIdempotencyKeyLen+1)
	if _, err := svc.BeginInstall(context.Background(), beginParams(t, long)); !errors.Is(err, ErrIdempotencyKeyTooLong) {
		t.Errorf("long key err = %v, want ErrIdempotencyKeyTooLong", err)
	}
}

// Without a provider or a box the scan flow cannot run, and begin must say so
// rather than create a session no worker can drive.
func TestBeginInstall_UnconfiguredIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]*InstallService{
		"no provider": newInstallService(newFakeInstallStore(), noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{Box: testBox(t)}, nil),
		"no box":      newInstallService(newFakeInstallStore(), noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{Provider: &fakeProvider{}}, nil),
		"no binder":   newInstallService(newFakeInstallStore(), noopTxStarter{}, nil, InstallServiceConfig{Provider: &fakeProvider{}, Box: testBox(t)}, nil),
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if svc.Configured() {
				t.Fatal("Configured() must be false")
			}
			if _, err := svc.BeginInstall(context.Background(), beginParams(t, "key-1")); !errors.Is(err, ErrInstallNotConfigured) {
				t.Errorf("err = %v, want ErrInstallNotConfigured", err)
			}
		})
	}
}

func TestGetSession_DecryptsQROnlyWhenAsked(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{
		Provider: &fakeProvider{}, Box: box,
	}, nil)

	const qr = "https://work.weixin.qq.com/ai/qc/scan?scode=secret-scode"
	enc, err := sealAndEncode(box, []byte(qr))
	if err != nil {
		t.Fatalf("sealAndEncode: %v", err)
	}
	id := installUUID(7)
	ws := installUUID(1)
	store.sessions[util.UUIDToString(id)] = db.WecomInstallSession{
		ID:                 id,
		WorkspaceID:        ws,
		AgentID:            installUUID(2),
		InitiatorUserID:    installUUID(3),
		Status:             InstallStatusPending,
		QrCodeUrlEncrypted: pgtype.Text{String: enc, Valid: true},
	}

	withoutQR, err := svc.GetSession(context.Background(), ws, id, false)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if withoutQR.QRCodeURL != "" {
		t.Error("decryptQR=false must not return the QR URL")
	}
	withQR, err := svc.GetSession(context.Background(), ws, id, true)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if withQR.QRCodeURL != qr {
		t.Errorf("QRCodeURL = %q, want the decrypted URL", withQR.QRCodeURL)
	}
}

// A session id from another workspace must read as not-found, so an id cannot be
// used to enumerate across tenants.
func TestGetSession_CrossWorkspaceIsNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
	id := installUUID(7)
	store.sessions[util.UUIDToString(id)] = db.WecomInstallSession{
		ID:          id,
		WorkspaceID: installUUID(1),
		Status:      InstallStatusPending,
	}

	if _, err := svc.GetSession(context.Background(), installUUID(5), id, false); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
	if _, err := svc.GetSession(context.Background(), installUUID(1), installUUID(6), false); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("unknown id err = %v, want ErrSessionNotFound", err)
	}
}

func newTestWorker(t *testing.T, svc *InstallService) *InstallWorker {
	t.Helper()
	return NewInstallWorker(svc, InstallWorkerConfig{})
}

// creating → pending: the worker fetches the QR, seals it, and schedules the
// next poll. The plaintext scode and QR URL must not reach the row.
func TestInstallWorker_CreatingFetchesAndSealsTheQR(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	provider := &fakeProvider{gen: GenerateResult{
		Scode:   "secret-scode",
		AuthURL: "https://work.weixin.qq.com/ai/qc/scan?scode=secret-scode",
	}}
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{
		Provider: provider, Box: box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:          installUUID(7),
		WorkspaceID: installUUID(1),
		AgentID:     installUUID(2),
		Status:      InstallStatusCreating,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}}

	worked, err := w.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if !worked {
		t.Error("processOne reported no work despite a claimed row")
	}
	if provider.genCalls != 1 {
		t.Errorf("Generate calls = %d, want 1", provider.genCalls)
	}
	if len(store.deferred) != 1 {
		t.Fatalf("deferred %d times, want 1", len(store.deferred))
	}
	d := store.deferred[0]
	if d.Status != InstallStatusPending {
		t.Errorf("status = %q, want pending", d.Status)
	}
	if !d.ExpiresAt.Valid || !d.ExpiresAt.Time.After(time.Now()) {
		t.Error("pending must carry a future expires_at")
	}
	// Both columns are ciphertext: together they let anyone finish creating the
	// bot, so the plaintext must not be readable off the row.
	for name, col := range map[string]pgtype.Text{"scode": d.ScodeEncrypted, "qr_code_url": d.QrCodeUrlEncrypted} {
		if !col.Valid || col.String == "" {
			t.Fatalf("%s was not written", name)
		}
		if strings.Contains(col.String, "secret-scode") {
			t.Errorf("%s stored plaintext", name)
		}
	}
	if plain, err := decodeAndOpen(box, d.QrCodeUrlEncrypted.String); err != nil || !strings.Contains(string(plain), "secret-scode") {
		t.Errorf("sealed qr_code_url did not round-trip: %v", err)
	}
}

// A generate failure retries rather than terminating: the deadline guard, not the
// first error, is what ends the session.
func TestInstallWorker_GenerateFailureRetriesUntilTheDeadline(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{
		Provider: &fakeProvider{genErr: errors.New("wecom down")}, Box: testBox(t),
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:          installUUID(7),
		WorkspaceID: installUUID(1),
		Status:      InstallStatusCreating,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}}
	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.failed) != 0 {
		t.Error("a single generate failure must not terminate the session")
	}
	if len(store.deferred) != 1 || store.deferred[0].Status != InstallStatusCreating {
		t.Error("a generate failure must defer, still in creating")
	}

	// Past the deadline, the same failure is terminal.
	store.deferred = nil
	store.claimQueue = []db.WecomInstallSession{{
		ID:          installUUID(7),
		WorkspaceID: installUUID(1),
		Status:      InstallStatusCreating,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now().Add(-2 * defaultGenerateDeadline), Valid: true},
	}}
	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.failed) != 1 || store.failed[0].ErrorReason.String != InstallErrorGenerateFailed {
		t.Errorf("failed = %+v, want one generate_failed", store.failed)
	}
}

// pending + success: the bot is bound through BotBinder and the session settles.
func TestInstallWorker_SuccessBindsTheBotAndCompletes(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	scodeEnc, err := sealAndEncode(box, []byte("secret-scode"))
	if err != nil {
		t.Fatalf("sealAndEncode: %v", err)
	}
	binder := &fakeBotBinder{inst: Installation{ID: installUUID(8)}}
	svc := newInstallService(store, noopTxStarter{}, binder, InstallServiceConfig{
		Provider: &fakeProvider{query: QueryResult{
			Status:  QueryStatusSuccess,
			BotInfo: &BotInfo{BotID: "bot-1", Secret: "sekrit"},
		}},
		Box: box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:              installUUID(7),
		WorkspaceID:     installUUID(1),
		AgentID:         installUUID(2),
		InitiatorUserID: installUUID(3),
		Status:          InstallStatusPending,
		ScodeEncrypted:  pgtype.Text{String: scodeEnc, Valid: true},
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}}

	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(binder.calls) != 1 {
		t.Fatalf("bind calls = %d, want 1", len(binder.calls))
	}
	bind := binder.calls[0]
	if bind.BotID != "bot-1" || bind.Secret != "sekrit" {
		t.Errorf("bind params = %+v, want the credentials WeCom returned", bind)
	}
	// The installer is the person who started the session, not whoever polled.
	if bind.InstallerUserID != installUUID(3) {
		t.Error("bind must attribute the install to the session initiator")
	}
	if len(store.completed) != 1 || store.completed[0].InstallationID != installUUID(8) {
		t.Errorf("completed = %+v, want the bound installation id", store.completed)
	}
	if len(store.failed) != 0 {
		t.Error("a successful bind must not fail the session")
	}
}

// A bind conflict is terminal with an actionable reason: the bot exists on
// WeCom's side either way, so retrying into the same conflict helps nobody.
func TestInstallWorker_BindConflictTerminatesTheSession(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	scodeEnc, _ := sealAndEncode(box, []byte("secret-scode"))
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{err: ErrBotOwnedByAnotherWorkspace}, InstallServiceConfig{
		Provider: &fakeProvider{query: QueryResult{
			Status:  QueryStatusSuccess,
			BotInfo: &BotInfo{BotID: "bot-1", Secret: "sekrit"},
		}},
		Box: box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:             installUUID(7),
		WorkspaceID:    installUUID(1),
		Status:         InstallStatusPending,
		ScodeEncrypted: pgtype.Text{String: scodeEnc, Valid: true},
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}}

	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.failed) != 1 || store.failed[0].ErrorReason.String != InstallErrorInstallationConflict {
		t.Errorf("failed = %+v, want one installation_conflict", store.failed)
	}
	if len(store.completed) != 0 {
		t.Error("a failed bind must not complete the session")
	}
}

// A success WeCom reports without credentials is a protocol error, not a bind.
func TestInstallWorker_SuccessWithoutCredentialsIsAProtocolError(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	scodeEnc, _ := sealAndEncode(box, []byte("secret-scode"))
	binder := &fakeBotBinder{}
	svc := newInstallService(store, noopTxStarter{}, binder, InstallServiceConfig{
		Provider: &fakeProvider{query: QueryResult{Status: QueryStatusSuccess}},
		Box:      box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:             installUUID(7),
		WorkspaceID:    installUUID(1),
		Status:         InstallStatusPending,
		ScodeEncrypted: pgtype.Text{String: scodeEnc, Valid: true},
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}}

	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(binder.calls) != 0 {
		t.Error("no credentials means nothing to bind")
	}
	if len(store.failed) != 1 || store.failed[0].ErrorReason.String != InstallErrorWecomProtocolError {
		t.Errorf("failed = %+v, want one wecom_protocol_error", store.failed)
	}
}

// An expired QR terminates rather than polling a scode WeCom will never accept.
func TestInstallWorker_ExpiredSessionTerminates(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	box := testBox(t)
	scodeEnc, _ := sealAndEncode(box, []byte("secret-scode"))
	provider := &fakeProvider{query: QueryResult{Status: QueryStatusPending}}
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{
		Provider: provider, Box: box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:             installUUID(7),
		WorkspaceID:    installUUID(1),
		Status:         InstallStatusPending,
		ScodeEncrypted: pgtype.Text{String: scodeEnc, Valid: true},
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(-time.Second), Valid: true},
	}}

	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if provider.queryCall != 0 {
		t.Error("an expired session must not be polled upstream")
	}
	if len(store.failed) != 1 || store.failed[0].ErrorReason.String != InstallErrorExpired {
		t.Errorf("failed = %+v, want one expired", store.failed)
	}
}

// Maintenance mode: with the integration unconfigured, a claimed session must
// still terminate so the admin's dialog shows an error instead of spinning.
func TestInstallWorker_MaintenanceModeTerminatesSessions(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	// No provider: Configured() is false.
	svc := newInstallService(store, noopTxStarter{}, &fakeBotBinder{}, InstallServiceConfig{
		Box: testBox(t),
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:          installUUID(7),
		WorkspaceID: installUUID(1),
		Status:      InstallStatusCreating,
		CreatedAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}}
	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.failed) != 1 || store.failed[0].ErrorReason.String != InstallErrorIntegrationUnconfigured {
		t.Errorf("failed = %+v, want one integration_unconfigured", store.failed)
	}
}

// Losing the lease mid-finalize must not complete the session: another worker
// owns it. The bind is idempotent, so nothing is left over.
func TestInstallWorker_LostLeaseDoesNotComplete(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	store.completeRows = 0 // the lease-token predicate matched no row
	box := testBox(t)
	scodeEnc, _ := sealAndEncode(box, []byte("secret-scode"))
	binder := &fakeBotBinder{inst: Installation{ID: installUUID(8)}}
	svc := newInstallService(store, noopTxStarter{}, binder, InstallServiceConfig{
		Provider: &fakeProvider{query: QueryResult{
			Status:  QueryStatusSuccess,
			BotInfo: &BotInfo{BotID: "bot-1", Secret: "sekrit"},
		}},
		Box: box,
	}, nil)
	w := newTestWorker(t, svc)

	store.claimQueue = []db.WecomInstallSession{{
		ID:             installUUID(7),
		WorkspaceID:    installUUID(1),
		Status:         InstallStatusPending,
		ScodeEncrypted: pgtype.Text{String: scodeEnc, Valid: true},
		ExpiresAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	}}

	if _, err := w.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if len(store.failed) != 0 {
		t.Error("losing the lease is not a failure of the session")
	}
}

func TestInstallWorker_EmptyQueueReportsNoWork(t *testing.T) {
	t.Parallel()
	store := newFakeInstallStore()
	svc := newTestInstallService(t, store, &fakeProvider{}, &fakeBotBinder{})
	worked, err := newTestWorker(t, svc).processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne: %v", err)
	}
	if worked {
		t.Error("an empty queue must report no work")
	}
}

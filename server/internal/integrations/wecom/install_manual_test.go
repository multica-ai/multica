package wecom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// manualTestService builds an InstallService wired for manual install with an
// injected Verify hook. verifyErr is what the probe returns; a nil hook would
// dial the real WeCom endpoint, which no default test may ever do.
func manualTestService(
	t *testing.T,
	store *fakeInstallStore,
	verifyErr error,
) (*InstallService, *int) {
	t.Helper()
	calls := 0
	svc := newInstallService(store, &fakeTxStarter{}, InstallServiceConfig{
		// Deliberately no SourceID / Provider: manual install must work on a
		// deployment that never provisioned a WeCom scan source.
		Box: testBox(t),
		Verify: func(_ context.Context, botID, secret string) error {
			calls++
			if botID == "" || secret == "" {
				t.Errorf("Verify called with empty credentials: bot=%q secret=%q", botID, secret)
			}
			return verifyErr
		},
	}, nil)
	return svc, &calls
}

func manualParams(t *testing.T) InstallManualParams {
	t.Helper()
	return InstallManualParams{
		WorkspaceID: mustParseUUID(t, testWS),
		AgentID:     mustParseUUID(t, testAgent1),
		InitiatorID: mustParseUUID(t, testUser1),
		BotID:       "bot-manual-1",
		Secret:      "sec-manual-1",
	}
}

func TestInstallManual_HappyPath(t *testing.T) {
	store := newFakeStore()
	svc, verifyCalls := manualTestService(t, store, nil)

	inst, err := svc.InstallManual(context.Background(), manualParams(t))
	if err != nil {
		t.Fatalf("InstallManual: %v", err)
	}
	if *verifyCalls != 1 {
		t.Fatalf("Verify calls = %d, want 1", *verifyCalls)
	}
	if !store.reclaimCalled {
		t.Fatal("expected the dead-owner reclaim gate to run before the upsert")
	}
	cfg, err := UnmarshalInstallationConfig(inst.Config)
	if err != nil {
		t.Fatalf("decode installation config: %v", err)
	}
	if cfg.AppID != "bot-manual-1" {
		t.Fatalf("app_id = %q, want bot-manual-1", cfg.AppID)
	}
	// The secret must be sealed, never stored in the clear.
	if cfg.SecretEncrypted == "" || strings.Contains(cfg.SecretEncrypted, "sec-manual-1") {
		t.Fatalf("secret_encrypted = %q, want sealed ciphertext", cfg.SecretEncrypted)
	}
	plain, err := decodeAndOpen(testBox(t), cfg.SecretEncrypted)
	if err != nil {
		t.Fatalf("open sealed secret: %v", err)
	}
	if string(plain) != "sec-manual-1" {
		t.Fatalf("sealed secret round-trip = %q, want sec-manual-1", plain)
	}
}

// TestInstallManual_TrimsCredentials: users paste from the WeCom console, which
// routinely carries trailing whitespace. A stored bot id with a stray space
// would never match an inbound event's app_id.
func TestInstallManual_TrimsCredentials(t *testing.T) {
	store := newFakeStore()
	svc, _ := manualTestService(t, store, nil)
	p := manualParams(t)
	p.BotID = "  bot-manual-1\t"
	p.Secret = " sec-manual-1 "

	inst, err := svc.InstallManual(context.Background(), p)
	if err != nil {
		t.Fatalf("InstallManual: %v", err)
	}
	cfg, err := UnmarshalInstallationConfig(inst.Config)
	if err != nil {
		t.Fatalf("decode installation config: %v", err)
	}
	if cfg.AppID != "bot-manual-1" {
		t.Fatalf("app_id = %q, want the trimmed bot-manual-1", cfg.AppID)
	}
}

func TestInstallManual_RejectsInvalidCredentialShapes(t *testing.T) {
	cases := map[string]func(p *InstallManualParams){
		"empty bot id":       func(p *InstallManualParams) { p.BotID = "   " },
		"empty secret":       func(p *InstallManualParams) { p.Secret = "" },
		"bot id too long":    func(p *InstallManualParams) { p.BotID = strings.Repeat("a", maxBotCredentialLen+1) },
		"secret too long":    func(p *InstallManualParams) { p.Secret = strings.Repeat("a", maxBotCredentialLen+1) },
		"newline in bot id":  func(p *InstallManualParams) { p.BotID = "bot\nid" },
		"newline in secret":  func(p *InstallManualParams) { p.Secret = "sec\nret" },
		"tab inside secret":  func(p *InstallManualParams) { p.Secret = "sec\tret" },
		"nul byte in bot id": func(p *InstallManualParams) { p.BotID = "bot\x00id" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			svc, verifyCalls := manualTestService(t, store, nil)
			p := manualParams(t)
			mutate(&p)
			if _, err := svc.InstallManual(context.Background(), p); !errors.Is(err, ErrBotCredentialsRequired) {
				t.Fatalf("err = %v, want ErrBotCredentialsRequired", err)
			}
			if *verifyCalls != 0 {
				t.Fatal("a malformed submission must be rejected before the WeCom probe")
			}
		})
	}
}

// TestInstallManual_RejectedCredentials is the whole reason the probe exists:
// a wrong secret has to fail at submit time instead of producing an
// installation whose bot silently never answers.
func TestInstallManual_RejectedCredentials(t *testing.T) {
	store := newFakeStore()
	svc, _ := manualTestService(t, store, &AuthFailedError{Code: 40001, Msg: "invalid secret"})

	if _, err := svc.InstallManual(context.Background(), manualParams(t)); !errors.Is(err, ErrInvalidBotCredentials) {
		t.Fatalf("err = %v, want ErrInvalidBotCredentials", err)
	}
	if store.upsertedInstallation.ID.Valid {
		t.Fatal("no installation may be written when WeCom rejected the credentials")
	}
}

// TestInstallManual_VerifyUnavailableIsNotBadCredentials: a network failure
// says nothing about the secret. Reporting it as "wrong secret" would send the
// user chasing a credential problem that does not exist.
func TestInstallManual_VerifyUnavailableIsNotBadCredentials(t *testing.T) {
	store := newFakeStore()
	svc, _ := manualTestService(t, store, errors.New("dial tcp: connection refused"))

	_, err := svc.InstallManual(context.Background(), manualParams(t))
	if !errors.Is(err, ErrVerifyUnavailable) {
		t.Fatalf("err = %v, want ErrVerifyUnavailable", err)
	}
	if errors.Is(err, ErrInvalidBotCredentials) {
		t.Fatal("an unreachable endpoint must not be reported as bad credentials")
	}
	if store.upsertedInstallation.ID.Valid {
		t.Fatal("no installation may be written when verification did not complete")
	}
}

func TestInstallManual_AgentAlreadyConnected(t *testing.T) {
	store := newFakeStore()
	store.activeByAgent[agentPendingKey(mustParseUUID(t, testWS), mustParseUUID(t, testAgent1))] =
		db.ChannelInstallation{Status: "active"}
	svc, verifyCalls := manualTestService(t, store, nil)

	if _, err := svc.InstallManual(context.Background(), manualParams(t)); !errors.Is(err, ErrActiveInstallationExists) {
		t.Fatalf("err = %v, want ErrActiveInstallationExists", err)
	}
	if *verifyCalls != 0 {
		t.Fatal("an already-connected agent must be refused before the WeCom probe")
	}
}

// TestInstallManual_BotIDConflictsAreClassified pins the accurate-message
// contract: "already in use" is not enough, the user needs to know whether they
// can fix it themselves (another agent here / an archived agent) or not
// (another workspace).
func TestInstallManual_BotIDConflictsAreClassified(t *testing.T) {
	otherWS := mustParseUUID(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
	cases := []struct {
		name  string
		owner db.GetChannelInstallationOwnerByAppIDRow
		want  error
	}{
		{
			name:  "another workspace",
			owner: db.GetChannelInstallationOwnerByAppIDRow{WorkspaceID: otherWS, AgentID: mustParseUUID(t, testAgent2)},
			want:  ErrBotIDOwnedByAnotherWorkspace,
		},
		{
			name: "archived agent",
			owner: db.GetChannelInstallationOwnerByAppIDRow{
				WorkspaceID:     mustParseUUID(t, testWS),
				AgentID:         mustParseUUID(t, testAgent2),
				AgentArchivedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
			want: ErrBotIDOwnedByArchivedAgent,
		},
		{
			name: "another agent in this workspace",
			owner: db.GetChannelInstallationOwnerByAppIDRow{
				WorkspaceID: mustParseUUID(t, testWS),
				AgentID:     mustParseUUID(t, testAgent2),
			},
			want: ErrBotIDOwnedInThisWorkspace,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStore()
			store.byAppID["bot-manual-1"] = db.ChannelInstallation{Status: "active"}
			store.ownerByAppID["bot-manual-1"] = tc.owner
			svc, verifyCalls := manualTestService(t, store, nil)

			if _, err := svc.InstallManual(context.Background(), manualParams(t)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if *verifyCalls != 0 {
				t.Fatal("a taken bot id must be refused before the WeCom probe")
			}
		})
	}
}

// TestInstallManual_RevokedBotIDIsReconnectable is the reconnect path: after a
// disconnect the row survives as revoked, and bindBot's reclaim gate frees the
// slot. Treating a revoked holder as a conflict would make a disconnected bot
// permanently un-reconnectable.
func TestInstallManual_RevokedBotIDIsReconnectable(t *testing.T) {
	store := newFakeStore()
	store.byAppID["bot-manual-1"] = db.ChannelInstallation{Status: "revoked"}
	svc, verifyCalls := manualTestService(t, store, nil)

	if _, err := svc.InstallManual(context.Background(), manualParams(t)); err != nil {
		t.Fatalf("InstallManual over a revoked row: %v", err)
	}
	if *verifyCalls != 1 {
		t.Fatalf("Verify calls = %d, want 1", *verifyCalls)
	}
}

// TestInstallManual_UpsertConflictResolvesLiveOwner covers the concurrent race
// the pre-check cannot close: both submitters read a free slot, one inserts,
// the other trips the (channel_type, app_id) unique index.
func TestInstallManual_UpsertConflictResolvesLiveOwner(t *testing.T) {
	otherWS := mustParseUUID(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
	store := newFakeStore()
	store.upsertErr = errors.New(`duplicate key value violates unique constraint "idx_channel_installation_type_appid"`)
	store.ownerByAppID["bot-manual-1"] = db.GetChannelInstallationOwnerByAppIDRow{
		WorkspaceID: otherWS,
		AgentID:     mustParseUUID(t, testAgent2),
	}
	svc, _ := manualTestService(t, store, nil)

	if _, err := svc.InstallManual(context.Background(), manualParams(t)); !errors.Is(err, ErrBotIDOwnedByAnotherWorkspace) {
		t.Fatalf("err = %v, want ErrBotIDOwnedByAnotherWorkspace", err)
	}
}

func TestManualConfigured(t *testing.T) {
	store := newFakeStore()

	// No box: a submitted secret cannot be sealed, so manual install is off.
	noBox := newInstallService(store, &fakeTxStarter{}, InstallServiceConfig{}, nil)
	if noBox.ManualConfigured() {
		t.Fatal("ManualConfigured() = true without a secretbox key")
	}
	if _, err := noBox.InstallManual(context.Background(), manualParams(t)); !errors.Is(err, ErrManualInstallUnsupported) {
		t.Fatalf("err = %v, want ErrManualInstallUnsupported", err)
	}

	// Box but no provider / source id: scan install is unavailable, manual is
	// not. This is the self-hosted deployment that never provisioned a WeCom
	// scan source.
	boxOnly := newInstallService(store, &fakeTxStarter{}, InstallServiceConfig{Box: testBox(t)}, nil)
	if boxOnly.Configured() {
		t.Fatal("Configured() = true without a provider; scan install should be off")
	}
	if !boxOnly.ManualConfigured() {
		t.Fatal("ManualConfigured() = false with a secretbox key present")
	}
}

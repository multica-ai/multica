package wecom

// installation_principal_test.go — guards config->>'principal_user_id' across a
// re-install.
//
// Upsert replaces channel_installation.config wholesale, and the config carries
// more than the install dialog collects. Everything the dialog does not ask for
// has to be read off the current row and written back, or the next re-install
// erases it. These tests drive InstallationService.Upsert — the only write path
// to a wecom row — against a real migrated database, the same way
// installation_reclaim_test.go does, and skip when none is reachable.

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wcPrBotRotated = "bot_principal_rotated"
	wcPrBotOther   = "bot_principal_other_agent"

	// The colleague the bot is operated on behalf of — not the installer.
	wcPrColleague = "5c09e200-0000-4000-8000-0000000000c1"
)

func cleanPrincipal(ctx context.Context, pool *pgxpool.Pool) {
	apps := []string{wcPrBotRotated, wcPrBotOther}
	_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`, apps)
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wcRclWS)
}

// setupPrincipal reuses the reclaim suite's workspace/agent fixtures and adds a
// cleanup for this file's own bot ids: the (channel_type, app_id) index is
// unique with no status condition, so a row left behind fails the next run.
func setupPrincipal(t *testing.T) (context.Context, *pgxpool.Pool, *InstallationService, *secretbox.Box) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	cleanPrincipal(ctx, pool)
	seedReclaimOwners(t, ctx, pool)
	t.Cleanup(func() { cleanPrincipal(ctx, pool) })

	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	// The probe is mandatory (see credential_probe.go) and the default one
	// dials WeCom for real. These tests are about what the config JSONB keeps
	// across a re-install, not about the proof of control, so they use the
	// same always-succeeds fake as installation_reclaim_test.go.
	svc, err := NewInstallationService(db.New(pool), pool, box, WithCredentialProbe(&fakeProbe{}))
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	return ctx, pool, svc, box
}

// insertLegacyWecomInstall writes a row in exactly the config shape this
// adapter produced before principal_user_id existed — no such key at all — so
// the decode path is exercised against a real pre-change row rather than one
// this build wrote.
func insertLegacyWecomInstall(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app, agent string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id, status)
VALUES ($1, $2, 'wecom', jsonb_build_object('app_id', $3::text, 'bot_id', $3::text, 'secret_encrypted', 'b2xk'), $4, 'active')`,
		wcRclWS, agent, app, wcRclInstall); err != nil {
		t.Fatalf("insert legacy install app=%s: %v", app, err)
	}
}

// setPrincipal is the only way a principal gets onto a row today: by hand.
// There is no dialog field for it, which is exactly why losing it on a
// re-install cannot be undone from the UI.
func setPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app, userID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE channel_installation SET config = config || jsonb_build_object('principal_user_id', $2::text)
WHERE channel_type = 'wecom' AND config->>'app_id' = $1`, app, userID); err != nil {
		t.Fatalf("set principal on %s: %v", app, err)
	}
}

func principalInDB(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app string) string {
	t.Helper()
	var got *string
	if err := pool.QueryRow(ctx,
		`SELECT config->>'principal_user_id' FROM channel_installation WHERE channel_type = 'wecom' AND config->>'app_id' = $1`,
		app).Scan(&got); err != nil {
		t.Fatalf("read principal for %s: %v", app, err)
	}
	if got == nil {
		return ""
	}
	return *got
}

// Rotating a leaked secret must not reassign the bot.
//
// The admin opens Settings, pastes a fresh secret and saves. That call knows
// nothing about principal_user_id — the dialog has no field for it — so
// rebuilding the config from the request alone drops the key, and from the
// next message on the bot belongs to whoever clicked the button. Nothing on
// screen says so and nothing in the log does either.
func TestUpsert_RotatingTheSecretKeepsThePrincipal(t *testing.T) {
	ctx, pool, svc, box := setupPrincipal(t)

	// A row that predates this field, then given a principal by hand.
	insertLegacyWecomInstall(t, ctx, pool, wcPrBotRotated, wcRclAgentA)
	setPrincipal(t, ctx, pool, wcPrBotRotated, wcPrColleague)

	p := params(wcPrBotRotated, wcRclAgentA)
	p.Secret = "rotated-secret"
	again, err := svc.Upsert(ctx, p)
	if err != nil {
		t.Fatalf("rotating the secret: %v", err)
	}

	if again.PrincipalUserID != wcPrColleague {
		t.Fatalf("principal = %q after a secret rotation, want it carried forward as %q — "+
			"the bot silently changed hands to whoever pressed save",
			again.PrincipalUserID, wcPrColleague)
	}
	if got := principalInDB(t, ctx, pool, wcPrBotRotated); got != wcPrColleague {
		t.Fatalf("principal in the stored config = %q, want %q", got, wcPrColleague)
	}

	// And the rotation itself still landed: carrying the principal forward
	// must not carry the old secret with it.
	creds, err := (&SecretboxCredentialsResolver{Box: box}).Credentials(again)
	if err != nil {
		t.Fatalf("unseal the stored secret: %v", err)
	}
	if creds.Secret != "rotated-secret" {
		t.Fatalf("stored secret = %q, want the rotated one — the rotation did not take", creds.Secret)
	}
}

// A first install on one agent must not pick up another agent's principal.
//
// There is no query keyed on (workspace, agent, channel_type), so the
// carry-forward reads the workspace's wecom rows and selects on agent_id. Get
// that filter wrong and installing a bot for agent B hands it to whoever agent
// A's bot answers for — a person the installing admin may not even know is
// involved.
func TestUpsert_FirstInstallDoesNotInheritAnotherAgentsPrincipal(t *testing.T) {
	ctx, pool, svc, _ := setupPrincipal(t)

	// Agent A already has a bot, operated on a colleague's behalf.
	insertLegacyWecomInstall(t, ctx, pool, wcPrBotRotated, wcRclAgentA)
	setPrincipal(t, ctx, pool, wcPrBotRotated, wcPrColleague)

	// Agent B gets its first bot, installed by the admin for themselves.
	fresh, err := svc.Upsert(ctx, params(wcPrBotOther, wcRclAgentB))
	if err != nil {
		t.Fatalf("first install on a second agent: %v", err)
	}
	if fresh.PrincipalUserID != "" {
		t.Fatalf("principal = %q on a first install, want empty (the installer) — "+
			"agent B's bot was handed to agent A's principal", fresh.PrincipalUserID)
	}
	if got := principalInDB(t, ctx, pool, wcPrBotOther); got != "" {
		t.Fatalf("principal in the stored config = %q, want the key absent", got)
	}
	// Agent A is untouched by an install that was never about it.
	if got := principalInDB(t, ctx, pool, wcPrBotRotated); got != wcPrColleague {
		t.Fatalf("agent A's principal = %q after an unrelated install, want %q", got, wcPrColleague)
	}
}

// The JSONB key is a compatibility surface: every row in the table was written
// without it. omitempty is what keeps those rows — and every future install
// that never sets a principal — encoding to the same bytes as before.
//
// No database needed; this is the encode/decode pair on its own.
func TestInstallConfig_PrincipalKeyIsAbsentUntilSet(t *testing.T) {
	// A config this build writes for an ordinary install carries no
	// principal_user_id at all. Were it emitted as "", every re-install would
	// rewrite rows with a key that means nothing, and any future reader that
	// distinguishes unset from set loses the distinction on day one.
	raw, err := encodeInstallConfig(Installation{BotID: "BOT-1", SecretEncrypted: []byte("sealed")})
	if err != nil {
		t.Fatalf("encodeInstallConfig: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("unmarshal encoded config: %v", err)
	}
	if _, present := keys["principal_user_id"]; present {
		t.Errorf("an install that sets no principal wrote principal_user_id anyway: %s", raw)
	}

	// A row written before this field existed still decodes, and reads as
	// "the installer" rather than failing or losing its other fields.
	legacy := []byte(`{"app_id":"BOT-1","bot_id":"BOT-1","secret_encrypted":"c2VhbGVk"}`)
	inst, err := installationFromRow(db.ChannelInstallation{Config: legacy, Status: string(InstallationActive)})
	if err != nil {
		t.Fatalf("decoding a pre-change installation row: %v", err)
	}
	if inst.PrincipalUserID != "" {
		t.Errorf("PrincipalUserID = %q on a pre-change row, want empty", inst.PrincipalUserID)
	}
	if inst.BotID != "BOT-1" || !bytes.Equal(inst.SecretEncrypted, []byte("sealed")) {
		t.Errorf("pre-change row lost its other fields: bot=%q secret=%q", inst.BotID, inst.SecretEncrypted)
	}

	// And a principal that IS set round-trips.
	raw, err = encodeInstallConfig(Installation{BotID: "BOT-1", PrincipalUserID: wcPrColleague})
	if err != nil {
		t.Fatalf("encodeInstallConfig with a principal: %v", err)
	}
	back, err := installationFromRow(db.ChannelInstallation{Config: raw})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.PrincipalUserID != wcPrColleague {
		t.Errorf("PrincipalUserID = %q, want it back as written (%q)", back.PrincipalUserID, wcPrColleague)
	}
}

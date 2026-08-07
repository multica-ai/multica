package wecom

// installation_bot_name_test.go — the bot's chat name survives a re-install and
// does not survive a bot swap. DB-backed, because the whole point is what the
// config JSONB holds after the upsert; skips when no migrated database is
// reachable, like installation_reclaim_test.go beside it.
//
// Why it matters: the name is optional in the Connect dialog, so an admin
// rotating a leaked secret leaves it blank. Blanking the stored name on that
// write puts group slash commands back to the whitespace guess this field
// exists to replace — silently, months after anyone typed it.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wcNameWS      = "5c09e201-0000-4000-8000-000000000001"
	wcNameRuntime = "5c09e201-0000-4000-8000-000000000003"
	wcNameAgent   = "5c09e201-0000-4000-8000-00000000000a"
	wcNameUser    = "5c09e201-0000-4000-8000-000000000005"

	wcNameBotA = "bot_name_a"
	wcNameBotB = "bot_name_b"
)

func setupBotName(t *testing.T) (context.Context, *pgxpool.Pool, *InstallationService) {
	t.Helper()
	pool := reclaimTestDB(t)
	ctx := context.Background()
	clean := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE config->>'app_id' = ANY($1)`,
			[]string{wcNameBotA, wcNameBotB})
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wcNameWS)
	}
	clean()
	exec := func(q string, args ...any) {
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("seed bot-name fixture: %v", err)
		}
	}
	exec(`INSERT INTO workspace (id, name, slug, description) VALUES ($1, 'wecom bot name', 'wecom-bot-name', '') ON CONFLICT (id) DO NOTHING`, wcNameWS)
	exec(`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider)
VALUES ($1, $2, 'wecom bot name runtime', 'local', 'multica_daemon') ON CONFLICT (id) DO NOTHING`, wcNameRuntime, wcNameWS)
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id)
VALUES ($1, $2, 'wecom bot name agent', 'local', $3) ON CONFLICT (id) DO NOTHING`, wcNameAgent, wcNameWS, wcNameRuntime)
	t.Cleanup(clean)
	// The probe is the reclaim tests' always-succeeds fake; these tests are
	// about what the config JSONB holds, not about the proof of control.
	svc, _ := newReclaimSvc(t, pool)
	return ctx, pool, svc
}

func botNameParams(bot, displayName string) InstallationParams {
	return InstallationParams{
		WorkspaceID:     mustUUID(wcNameWS),
		AgentID:         mustUUID(wcNameAgent),
		InstallerUserID: mustUUID(wcNameUser),
		BotID:           bot,
		Secret:          "s3cr3t",
		BotDisplayName:  displayName,
	}
}

// TestRotatingTheSecretKeepsTheBotName: the admin re-opens Connect to paste a
// fresh secret and does not retype the optional name. Losing it here breaks
// every group slash command for a bot whose name has a space in it, with
// nothing on screen to say so.
func TestRotatingTheSecretKeepsTheBotName(t *testing.T) {
	ctx, _, svc := setupBotName(t)

	if _, err := svc.Upsert(ctx, botNameParams(wcNameBotA, "Multica Bot")); err != nil {
		t.Fatalf("first install: %v", err)
	}
	again, err := svc.Upsert(ctx, botNameParams(wcNameBotA, ""))
	if err != nil {
		t.Fatalf("secret rotation: %v", err)
	}
	if again.BotDisplayName != "Multica Bot" {
		t.Fatalf("BotDisplayName after rotation = %q, want it kept — a blank optional field "+
			"erased the name, and every group slash command goes back to the whitespace guess", again.BotDisplayName)
	}
}

// TestRenamingTheBotOverwritesTheStoredName: the carry-forward must not make
// the field unchangeable. Renaming in the WeCom console and retyping it here
// has to take effect.
func TestRenamingTheBotOverwritesTheStoredName(t *testing.T) {
	ctx, _, svc := setupBotName(t)

	if _, err := svc.Upsert(ctx, botNameParams(wcNameBotA, "Multica Bot")); err != nil {
		t.Fatalf("first install: %v", err)
	}
	renamed, err := svc.Upsert(ctx, botNameParams(wcNameBotA, "Acme Support Bot"))
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.BotDisplayName != "Acme Support Bot" {
		t.Fatalf("BotDisplayName after rename = %q, want Acme Support Bot", renamed.BotDisplayName)
	}
}

// TestSwappingTheBotDoesNotInheritTheOldName: swapping which bot an agent uses
// reuses the same installation row. Carrying the name across would make the new
// bot strip a mention that belongs to a bot nobody is talking to.
func TestSwappingTheBotDoesNotInheritTheOldName(t *testing.T) {
	ctx, _, svc := setupBotName(t)

	if _, err := svc.Upsert(ctx, botNameParams(wcNameBotA, "Multica Bot")); err != nil {
		t.Fatalf("first install: %v", err)
	}
	swapped, err := svc.Upsert(ctx, botNameParams(wcNameBotB, ""))
	if err != nil {
		t.Fatalf("bot swap: %v", err)
	}
	if swapped.BotDisplayName != "" {
		t.Fatalf("the new bot inherited the previous bot's chat name %q", swapped.BotDisplayName)
	}
}

// TestTheBotNameSurvivesTheConfigRoundTrip: the field has to reach the JSONB
// and come back, or nothing above proves anything about what the WS connector
// reads at boot.
func TestTheBotNameSurvivesTheConfigRoundTrip(t *testing.T) {
	t.Parallel()
	cfg, err := encodeInstallConfig(Installation{BotID: "bot-xyz", BotDisplayName: "Multica Bot"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := installationFromRow(db.ChannelInstallation{Config: cfg, Status: "active"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BotDisplayName != "Multica Bot" {
		t.Fatalf("BotDisplayName round trip = %q, want Multica Bot", out.BotDisplayName)
	}
}

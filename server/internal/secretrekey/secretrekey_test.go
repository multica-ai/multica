package secretrekey

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func mustBox(t *testing.T, key []byte, previous ...[]byte) *secretbox.Box {
	t.Helper()
	b, err := secretbox.New(key, previous...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustSeal(t *testing.T, b *secretbox.Box, s string) []byte {
	t.Helper()
	sealed, err := b.Seal([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

// rekeyFixture stores one value per encoding sealed under the old key, plus
// one value sealed under an unrelated key, then exposes them to assertions.
type rekeyFixture struct {
	pool                         *pgxpool.Pool
	oldBox, newBox, newOnly      *secretbox.Box
	channelID, vcsID, appConnID  string
	strangerChannelID, workspace string
}

func setupRekey(t *testing.T) rekeyFixture {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := testutil.IsolatedDB(t, os.Getenv("DATABASE_URL"))
	ctx := context.Background()

	oldKey, curKey := newKey(t), newKey(t)
	f := rekeyFixture{
		pool:      pool,
		oldBox:    mustBox(t, oldKey),
		newBox:    mustBox(t, curKey, oldKey),
		newOnly:   mustBox(t, curKey),
		workspace: uuid.NewString(),
	}
	stranger := mustBox(t, newKey(t))

	f.channelID = insertChannel(t, pool, f.workspace, "feishu", "app_secret_encrypted",
		base64.StdEncoding.EncodeToString(mustSeal(t, f.oldBox, "feishu-secret")))
	f.strangerChannelID = insertChannel(t, pool, f.workspace, "feishu", "app_secret_encrypted",
		base64.StdEncoding.EncodeToString(mustSeal(t, stranger, "other")))

	if err := pool.QueryRow(ctx, `INSERT INTO vcs_connection (workspace_id, instance_url, account_login, access_token_encrypted, webhook_secret_encrypted)
		VALUES ($1, 'https://git.example.test', 'bot', $2, $3) RETURNING id::text`,
		f.workspace,
		base64.StdEncoding.EncodeToString(mustSeal(t, f.oldBox, "vcs-token")),
		base64.StdEncoding.EncodeToString(mustSeal(t, f.newBox, "vcs-hook")),
	).Scan(&f.vcsID); err != nil {
		t.Fatalf("insert vcs_connection: %v", err)
	}

	if err := pool.QueryRow(ctx, `INSERT INTO remote_app_connection (app_slug, scope, owner_id, connected_by, oauth_client_id, access_token, refresh_token)
		VALUES ('app', 'team', $1, $1, $1, $2, NULL) RETURNING id::text`,
		f.workspace, mustSeal(t, f.oldBox, "app-access"),
	).Scan(&f.appConnID); err != nil {
		t.Fatalf("insert remote_app_connection: %v", err)
	}
	return f
}

func insertChannel(t *testing.T, pool *pgxpool.Pool, workspace, channelType, field, value string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO channel_installation (workspace_id, agent_id, channel_type, installer_user_id, config)
		VALUES ($1, gen_random_uuid(), $2, gen_random_uuid(), jsonb_build_object('app_id', gen_random_uuid()::text, 'tenant_key', 't', $3::text, $4::text))
		RETURNING id::text`, workspace, channelType, field, value).Scan(&id); err != nil {
		t.Fatalf("insert channel_installation: %v", err)
	}
	return id
}

func targetsFor(env string) []Target {
	var out []Target
	for _, tg := range Targets {
		out = append(out, tg)
		out[len(out)-1].KeyEnv = env
	}
	return out
}

func reportFor(t *testing.T, reports []Report, table, column, field string) Report {
	t.Helper()
	for _, r := range reports {
		if r.Target.Table == table && r.Target.Column == column && r.Target.JSONField == field && r.Skipped == "" {
			return r
		}
	}
	t.Fatalf("no report for %s.%s %s", table, column, field)
	return Report{}
}

func TestRunReportsThenReseals(t *testing.T) {
	f := setupRekey(t)
	ctx := context.Background()
	// One box for every target keeps the fixture independent of which
	// integration owns each table.
	targets := targetsFor("TEST_KEY")
	boxes := map[string]*secretbox.Box{"TEST_KEY": f.newBox}

	reports, err := Run(ctx, f.pool, targets, boxes, false)
	if err != nil {
		t.Fatalf("report run: %v", err)
	}
	feishu := reportFor(t, reports, "channel_installation", "config", "app_secret_encrypted")
	if feishu.Previous != 1 || feishu.Unreadable != 1 {
		t.Fatalf("feishu report = %+v, want 1 pending and 1 unreadable", feishu)
	}
	if got := feishu.UnreadableKeys; len(got) != 1 || got[0][0] != f.strangerChannelID {
		t.Fatalf("unreadable keys = %v, want [%s]", got, f.strangerChannelID)
	}
	// A report run writes nothing.
	if _, err := f.newOnly.Open(f.channelSecret(t)); err == nil {
		t.Fatal("report run re-sealed a value")
	}

	reports, err = Run(ctx, f.pool, targets, boxes, true)
	if err != nil {
		t.Fatalf("apply run: %v", err)
	}
	vcsHook := reportFor(t, reports, "vcs_connection", "webhook_secret_encrypted", "")
	if vcsHook.Current != 1 || vcsHook.Previous != 0 {
		t.Fatalf("vcs webhook report = %+v, want already current", vcsHook)
	}

	// After re-sealing, the new key alone opens every value it can own.
	assertOpens(t, f.newOnly, f.channelSecret(t), "feishu-secret")
	var vcsToken string
	var appAccess, appRefresh []byte
	if err := f.pool.QueryRow(ctx, `SELECT access_token_encrypted FROM vcs_connection WHERE id = $1`, f.vcsID).Scan(&vcsToken); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(vcsToken)
	if err != nil {
		t.Fatal(err)
	}
	assertOpens(t, f.newOnly, raw, "vcs-token")
	if err := f.pool.QueryRow(ctx, `SELECT access_token, refresh_token FROM remote_app_connection WHERE id = $1`, f.appConnID).Scan(&appAccess, &appRefresh); err != nil {
		t.Fatal(err)
	}
	assertOpens(t, f.newOnly, appAccess, "app-access")
	if appRefresh != nil {
		t.Fatalf("NULL refresh_token became %x", appRefresh)
	}

	// Other config fields survive; the unreadable row is left as it was.
	var appID, tenant string
	if err := f.pool.QueryRow(ctx, `SELECT config->>'app_id', config->>'tenant_key' FROM channel_installation WHERE id = $1`, f.channelID).Scan(&appID, &tenant); err != nil {
		t.Fatal(err)
	}
	if appID == "" || tenant != "t" {
		t.Fatalf("config fields lost: app_id=%q tenant_key=%q", appID, tenant)
	}

	reports, err = Run(ctx, f.pool, targets, boxes, false)
	if err != nil {
		t.Fatalf("verify run: %v", err)
	}
	for _, r := range reports {
		if r.Previous != 0 {
			t.Fatalf("%s still has %d values under the previous key", r.Target.Name(), r.Previous)
		}
	}
}

func TestRunFailsWithoutKey(t *testing.T) {
	f := setupRekey(t)
	_, err := Run(context.Background(), f.pool, targetsFor("UNSET_KEY"), map[string]*secretbox.Box{}, false)
	if err == nil {
		t.Fatal("Run without a key reported success")
	}
	if _, err := f.oldBox.Open(f.channelSecret(t)); err != nil {
		t.Fatalf("value changed without a key: %v", err)
	}
}

func (f rekeyFixture) channelSecret(t *testing.T) []byte {
	t.Helper()
	var enc string
	if err := f.pool.QueryRow(context.Background(), `SELECT config->>'app_secret_encrypted' FROM channel_installation WHERE id = $1`, f.channelID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertOpens(t *testing.T, b *secretbox.Box, sealed []byte, want string) {
	t.Helper()
	got, err := b.Open(sealed)
	if err != nil {
		t.Fatalf("open %q: %v", want, err)
	}
	if string(got) != want {
		t.Fatalf("open = %q, want %q", got, want)
	}
}

func TestTargetsReferenceKnownKeys(t *testing.T) {
	known := map[string]bool{}
	for _, env := range Integrations {
		known[env] = true
	}
	covered := map[string]bool{}
	for _, tg := range Targets {
		if !known[tg.KeyEnv] {
			t.Errorf("%s uses key env %s with no --integrations name", tg.Name(), tg.KeyEnv)
		}
		covered[tg.KeyEnv] = true
	}
	for name, env := range Integrations {
		if !covered[env] {
			t.Errorf("integration %s has no target", name)
		}
	}
}

func TestSelect(t *testing.T) {
	for _, names := range [][]string{nil, {""}, {" "}} {
		if _, _, err := Select(names); err == nil {
			t.Errorf("Select(%q) selected nothing without an error", names)
		}
	}
	if _, _, err := Select([]string{"lark", "plugin"}); err == nil {
		t.Error("Select accepted an unknown integration")
	}

	targets, envs, err := Select([]string{"vcs", " slack"})
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 || envs[0] != "MULTICA_SLACK_SECRET_KEY" || envs[1] != "MULTICA_VCS_SECRET_KEY" {
		t.Fatalf("envs = %v", envs)
	}
	for _, tg := range targets {
		if tg.KeyEnv != "MULTICA_SLACK_SECRET_KEY" && tg.KeyEnv != "MULTICA_VCS_SECRET_KEY" {
			t.Fatalf("selected %s outside slack and vcs", tg.Name())
		}
	}
	if len(targets) != 6 {
		t.Fatalf("selected %d targets, want 6 (2 slack + 4 vcs)", len(targets))
	}

	all, envs, err := Select([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(Targets) || len(envs) != len(Integrations) {
		t.Fatalf("all selected %d targets / %d keys, want %d / %d", len(all), len(envs), len(Targets), len(Integrations))
	}
}

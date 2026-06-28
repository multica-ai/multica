package toolpolicy

// Integration tests for the per-credential half of the table read model
// (table_credential.go), focused on FIR-1739's last mile: the Credentials tab
// must surface Agent Vault boxes — which live ONLY in Agent Vault and never get
// a cerebro_credential registry row — as grantable agentvault-vault:<name> rows.
// firtal is exactly this shape (empty cerebro_credential, 6 live Agent Vault
// boxes), so before the fix the tab rendered zero rows. They share the
// store_test.go fixture (TestMain) and skip cleanly when no test database is
// reachable.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
)

// fakeVaultLister is a static agentvault.VaultLister for tests: it returns a
// fixed set of box names (or a forced error) without talking to Agent Vault.
type fakeVaultLister struct {
	names []string
	err   error
}

func (f fakeVaultLister) ListVaults(_ context.Context) ([]agentvault.Vault, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]agentvault.Vault, len(f.names))
	for i, n := range f.names {
		out[i] = agentvault.Vault{ID: n, Name: n}
	}
	return out, nil
}

// clearCredentials drops every cerebro_credential row for the test workspace so
// each subtest controls the registered-credential universe exactly.
func clearCredentials(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(),
		`DELETE FROM cerebro_credential WHERE workspace_id = $1`, tpTestWorkspaceID); err != nil {
		t.Fatalf("clear credentials: %v", err)
	}
}

// addCredential inserts one registered credential (cerebro_credential row) and
// returns its id-scoped ResourcePattern (cerebro-credential:<uuid>).
func addCredential(t *testing.T, s *Store, name string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(context.Background(), `
		INSERT INTO cerebro_credential
			(workspace_id, type, name, encrypted_value, created_by_type, created_by_id)
		VALUES ($1, 'api_key', $2, '\x00', 'member', $3)
		RETURNING id::text
	`, tpTestWorkspaceID, name, tpTestUserID).Scan(&id); err != nil {
		t.Fatalf("insert credential %q: %v", name, err)
	}
	return credentialResourcePrefix + id
}

func findCredentialRow(rows []TableRow, key, pattern string) (TableRow, bool) {
	for _, r := range rows {
		if r.ToolKey == key && r.ResourcePattern == pattern {
			return r, true
		}
	}
	return TableRow{}, false
}

// TestTable_CredentialRowsFromAgentVault is the FIR-1739 core check: a workspace
// with an EMPTY cerebro_credential table but N Agent Vault boxes lists one row
// group per box, keyed on agentvault-vault:<name>, with Allow/Ask/Deny resolving
// independently per (capability, box) cell across layers — exactly the firtal
// case that previously rendered "No credentials available yet."
func TestTable_CredentialRowsFromAgentVault(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	clearCredentials(t, s)
	defer clearCredentials(t, s)
	ctx := context.Background()

	s.WithVaultLister(fakeVaultLister{names: []string{"bigquery", "cloudflare", "deployment"}})

	// One ordinary capability so we can assert credential rows coexist with the
	// capability-wide listing rather than replacing it.
	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	const (
		bq     = agentvault.VaultResourcePrefix + "bigquery"
		cf     = agentvault.VaultResourcePrefix + "cloudflare"
		deploy = agentvault.VaultResourcePrefix + "deployment"
	)
	agent, user := uuidByte(2), tpTestUserID
	// credential.reveal on bigquery: agent Allow → effective Allow on that cell.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: capCredentialReveal, Layer: LayerAgent, SubjectID: agent, ResourcePattern: bq, Setting: SettingAllow}); err != nil {
		t.Fatalf("set agent reveal bigquery: %v", err)
	}
	// credential.reveal on cloudflare: workspace Ask → effective Ask.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: capCredentialReveal, Layer: LayerWorkspace, SubjectID: tpTestWorkspaceID, ResourcePattern: cf, Setting: SettingAsk}); err != nil {
		t.Fatalf("set workspace ask cloudflare: %v", err)
	}
	// credential.rotate on deployment: user Deny → effective Deny capped by user.
	if _, err := s.Set(ctx, SetParams{WorkspaceID: tpTestWorkspaceID, ToolKey: capCredentialRotate, Layer: LayerUser, SubjectID: user, ResourcePattern: deploy, Setting: SettingDeny}); err != nil {
		t.Fatalf("set user deny rotate deployment: %v", err)
	}

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, AgentID: agent, UserID: user, IncludeCredentials: true})
	if err != nil {
		t.Fatalf("table: %v", err)
	}

	// 3 boxes × len(credentialCapabilities) credential rows, every one carrying a
	// agentvault-vault:<name> resource (no cerebro-credential:<uuid> here).
	credRows := 0
	for _, r := range rows {
		if r.Source == credentialSource {
			credRows++
			if _, ok := agentvault.VaultFromResourcePattern(r.ResourcePattern); !ok {
				t.Fatalf("credential row has non-vault resource %q, want agentvault-vault:<name>", r.ResourcePattern)
			}
		}
	}
	if want := 3 * len(credentialCapabilities); credRows != want {
		t.Fatalf("got %d credential rows, want %d (3 boxes × %d caps)", credRows, want, len(credentialCapabilities))
	}

	// The capability-wide row still lists — credential rows must not replace it.
	if _, ok := findRow(rows, "add_comment"); !ok {
		t.Fatal("capability-wide row add_comment missing — credential rows must not replace the listing")
	}

	// Authored cells resolve per layer.
	revealBQ, ok := findCredentialRow(rows, capCredentialReveal, bq)
	if !ok {
		t.Fatalf("credential.reveal row for bigquery missing")
	}
	if revealBQ.Category != "bigquery" || revealBQ.Source != credentialSource {
		t.Fatalf("bigquery row labels = %q/%q, want box name + credential source", revealBQ.Category, revealBQ.Source)
	}
	if revealBQ.Layers[LayerAgent] != SettingAllow || revealBQ.Effective.Setting != SettingAllow {
		t.Fatalf("reveal bigquery = layer %q / effective %q, want agent allow / allow", revealBQ.Layers[LayerAgent], revealBQ.Effective.Setting)
	}

	revealCF, ok := findCredentialRow(rows, capCredentialReveal, cf)
	if !ok {
		t.Fatalf("credential.reveal row for cloudflare missing")
	}
	if revealCF.Effective.Setting != SettingAsk {
		t.Fatalf("reveal cloudflare effective = %q, want ask", revealCF.Effective.Setting)
	}

	rotateDeploy, ok := findCredentialRow(rows, capCredentialRotate, deploy)
	if !ok {
		t.Fatalf("credential.rotate row for deployment missing")
	}
	if rotateDeploy.Effective.Setting != SettingDeny || rotateDeploy.Effective.CappedBy != LayerUser {
		t.Fatalf("rotate deployment effective = %q capped %q, want deny/user", rotateDeploy.Effective.Setting, rotateDeploy.Effective.CappedBy)
	}

	// A setting on one box must not leak onto another: cloudflare reveal is Ask,
	// so bigquery's authored Allow did not bleed across.
	if revealCF.Layers[LayerAgent] == SettingAllow {
		t.Fatal("agent allow on bigquery leaked onto cloudflare reveal")
	}
}

// TestTable_CredentialRowsDedupedAgainstRegistered proves a box that is BOTH
// registered in cerebro_credential AND present in Agent Vault is listed once,
// under its id-scoped cerebro-credential:<uuid> resource (the registered row
// wins), while Agent Vault-only boxes still surface as agentvault-vault:<name>.
func TestTable_CredentialRowsDedupedAgainstRegistered(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	clearCredentials(t, s)
	defer clearCredentials(t, s)
	ctx := context.Background()

	bqResource := addCredential(t, s, "bigquery")
	s.WithVaultLister(fakeVaultLister{names: []string{"bigquery", "cloudflare"}})

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, IncludeCredentials: true})
	if err != nil {
		t.Fatalf("table: %v", err)
	}

	// bigquery present once, id-scoped; the vault-scoped duplicate is suppressed.
	if _, ok := findCredentialRow(rows, capCredentialReveal, bqResource); !ok {
		t.Fatalf("registered bigquery row (%s) missing", bqResource)
	}
	if _, dup := findCredentialRow(rows, capCredentialReveal, agentvault.VaultResourcePrefix+"bigquery"); dup {
		t.Fatal("bigquery listed twice — vault duplicate not deduped against registered credential")
	}
	// cloudflare is Agent Vault-only → vault-scoped row.
	if _, ok := findCredentialRow(rows, capCredentialReveal, agentvault.VaultResourcePrefix+"cloudflare"); !ok {
		t.Fatal("Agent Vault-only cloudflare row missing")
	}

	// Exactly 2 boxes total.
	boxes := map[string]bool{}
	for _, r := range rows {
		if r.Source == credentialSource {
			boxes[r.Category] = true
		}
	}
	if len(boxes) != 2 {
		t.Fatalf("got %d credential boxes, want 2 (bigquery + cloudflare)", len(boxes))
	}
}

// TestTable_CredentialRowsNilListerNoVaultRows proves the table emits no
// vault-sourced rows when no lister is wired and cerebro_credential is empty —
// the degradation contract when Agent Vault admin creds are absent.
func TestTable_CredentialRowsNilListerNoVaultRows(t *testing.T) {
	s := newTPStore(t)
	clearAll(t, s)
	clearCaps(t, s)
	clearCredentials(t, s)
	defer clearCredentials(t, s)
	ctx := context.Background()

	addCap(t, s, "add_comment", "Add comment", "Issues", "builtin")

	rows, err := s.Table(ctx, TableQuery{WorkspaceID: tpTestWorkspaceID, IncludeCredentials: true})
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	for _, r := range rows {
		if r.Source == credentialSource {
			t.Fatalf("unexpected credential row with nil lister and empty registry: %+v", r)
		}
	}
}

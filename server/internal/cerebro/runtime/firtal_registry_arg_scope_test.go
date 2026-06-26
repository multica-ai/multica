package runtime

// FIR-2083 gate-seam proof. The data-source-scoping deny path must be proven
// where it is enforced — through chainGateDataSource with a LIVE resolver and
// the registry_grants_enabled flag ON — not only by unit composition of
// ConditionedSetting/Matches. This is the security guarantee: an agent calling
// `execute` on a source outside its selected set is actually blocked.
//
// It reuses the shared pool + workspace/runtime/user fixture from account_test.go's
// TestMain and skips cleanly when no DB is reachable.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRegistryGate_ArgAllowScopesDataSources is the headline FIR-2083 check: a
// capability-wide whitelist Allow conditioned on data_source_id ∈ {selected set}
// lets a selected source through chainGateDataSource and DENIES a non-selected
// one — a new source not in the set fails closed, never falling back to the
// Base=Allow default.
func TestRegistryGate_ArgAllowScopesDataSources(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping registry gate integration test")
	}
	pool := runtimeAccountTestPool
	ctx := context.Background()

	const allowedID = "finance-pnl"
	const deniedID = "hr-salaries"

	// An agent anchored to the test runtime/owner so the chain resolves its layer.
	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id
	`, runtimeAccountTestWSID, "registry-arg-scope-probe", runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Flip registry_grants_enabled ON for the workspace; restore on cleanup so we
	// do not leak the flag into other runtime tests that share this workspace.
	var prevSettings []byte
	_ = pool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, runtimeAccountTestWSID).Scan(&prevSettings)
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = '{"registry_grants_enabled": true}'::jsonb WHERE id = $1`, runtimeAccountTestWSID); err != nil {
		t.Fatalf("enable registry grants: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if len(prevSettings) > 0 {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = $2 WHERE id = $1`, runtimeAccountTestWSID, prevSettings)
		} else {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, runtimeAccountTestWSID)
		}
		_, _ = pool.Exec(bg, `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1`, runtimeAccountTestWSID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// The capability-wide scoping rule the When-popover authors: Allow
	// firtal_registry WHEN data_source_id ∈ {allowedID}. resource_pattern="" so it
	// applies across sources; the ArgAllow condition narrows which sources pass.
	store := toolpolicy.NewStore(pool)
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID:     runtimeAccountTestWSID,
		ToolKey:         "firtal_registry",
		Layer:           toolpolicy.LayerAgent,
		SubjectID:       agentID,
		Setting:         toolpolicy.SettingAllow,
		ResourcePattern: "",
		Conditions: &toolpolicy.Condition{
			ArgAllowlist: []toolpolicy.ArgAllow{{Arg: "data_source_id", Values: []string{allowedID}}},
		},
	}); err != nil {
		t.Fatalf("author scoping rule: %v", err)
	}

	tool := &FirtalRegistryTool{
		queries: db.New(pool),
		cerebro: cerebrodb.New(pool),
		tctx: ToolContext{
			WorkspaceID: runtimeAccountTestWSID,
			AgentID:     agentID,
			UserID:      runtimeAccountTestUserID,
		},
	}

	// A selected source passes the gate. baseURL="" skips folder resolution — this
	// rule scopes on data_source_id, so no registry round-trip is needed.
	if err := tool.chainGateDataSource(ctx, "execute", allowedID, "", ""); err != nil {
		t.Fatalf("selected source %q must pass the gate, got error: %v", allowedID, err)
	}

	// A non-selected source is denied at the gate — the whitelist closes to Deny.
	err := tool.chainGateDataSource(ctx, "execute", deniedID, "", "")
	if err == nil {
		t.Fatalf("non-selected source %q must be DENIED through chainGateDataSource, got allow", deniedID)
	}
	if !strings.Contains(err.Error(), deniedID) {
		t.Fatalf("deny error should name the blocked source %q, got: %v", deniedID, err)
	}
}

// TestRegistryGate_NoScopingRuleAllowsAll proves the change is behavior-preserving:
// with the flag ON but no scoping rule authored, every source passes (Base=Allow),
// so enabling the gate without configuring scope does not brick the registry.
func TestRegistryGate_NoScopingRuleAllowsAll(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping registry gate integration test")
	}
	pool := runtimeAccountTestPool
	ctx := context.Background()

	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id
	`, runtimeAccountTestWSID, "registry-arg-scope-noop-probe", runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var prevSettings []byte
	_ = pool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, runtimeAccountTestWSID).Scan(&prevSettings)
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = '{"registry_grants_enabled": true}'::jsonb WHERE id = $1`, runtimeAccountTestWSID); err != nil {
		t.Fatalf("enable registry grants: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if len(prevSettings) > 0 {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = $2 WHERE id = $1`, runtimeAccountTestWSID, prevSettings)
		} else {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, runtimeAccountTestWSID)
		}
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	tool := &FirtalRegistryTool{
		queries: db.New(pool),
		cerebro: cerebrodb.New(pool),
		tctx: ToolContext{
			WorkspaceID: runtimeAccountTestWSID,
			AgentID:     agentID,
			UserID:      runtimeAccountTestUserID,
		},
	}
	if err := tool.chainGateDataSource(ctx, "execute", "any-source", "", ""); err != nil {
		t.Fatalf("with no scoping rule, every source must pass; got error: %v", err)
	}
}

// TestRegistryGate_FolderScopeAutoCovers is the FIR-2083 folder-scoping headline:
// a rule conditioned on folder_id ∈ {a chosen folder} covers EVERY source in that
// folder — including one the rule never named — while denying a source in another
// folder. This is the "Finance · all" promise: drop a new source into Finance and
// it is covered without editing the rule. The gate resolves the called source's
// folder through the registry list endpoint, here a stub returning folder_id per id.
func TestRegistryGate_FolderScopeAutoCovers(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping registry gate integration test")
	}
	pool := runtimeAccountTestPool
	ctx := context.Background()

	const financeFolder = "folder-finance"
	const hrFolder = "folder-hr"
	const financeOld = "fin-pnl" // named nowhere — covered only via its folder
	const financeNew = "fin-newly-added"
	const hrSource = "hr-salaries"

	// Stub registry list endpoint: each source carries its folder_id, exactly as the
	// Phase 1 registry change now returns. The gate maps the called id → folder_id.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/registry/data-sources" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"` + financeOld + `","folder_id":"` + financeFolder + `"},
			{"id":"` + financeNew + `","folder_id":"` + financeFolder + `"},
			{"id":"` + hrSource + `","folder_id":"` + hrFolder + `"}
		]`))
	}))
	t.Cleanup(srv.Close)

	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id
	`, runtimeAccountTestWSID, "registry-folder-scope-probe", runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var prevSettings []byte
	_ = pool.QueryRow(ctx, `SELECT settings FROM workspace WHERE id = $1`, runtimeAccountTestWSID).Scan(&prevSettings)
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = '{"registry_grants_enabled": true}'::jsonb WHERE id = $1`, runtimeAccountTestWSID); err != nil {
		t.Fatalf("enable registry grants: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		if len(prevSettings) > 0 {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = $2 WHERE id = $1`, runtimeAccountTestWSID, prevSettings)
		} else {
			_, _ = pool.Exec(bg, `UPDATE workspace SET settings = '{}'::jsonb WHERE id = $1`, runtimeAccountTestWSID)
		}
		_, _ = pool.Exec(bg, `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1`, runtimeAccountTestWSID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// The folder rule the When-popover authors for "Finance · all": Allow
	// firtal_registry WHEN folder_id ∈ {financeFolder}. No source id is enumerated.
	store := toolpolicy.NewStore(pool)
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID:     runtimeAccountTestWSID,
		ToolKey:         "firtal_registry",
		Layer:           toolpolicy.LayerAgent,
		SubjectID:       agentID,
		Setting:         toolpolicy.SettingAllow,
		ResourcePattern: "",
		Conditions: &toolpolicy.Condition{
			ArgAllowlist: []toolpolicy.ArgAllow{{Arg: "folder_id", Values: []string{financeFolder}}},
		},
	}); err != nil {
		t.Fatalf("author folder rule: %v", err)
	}

	tool := &FirtalRegistryTool{
		queries: db.New(pool),
		cerebro: cerebrodb.New(pool),
		tctx: ToolContext{
			WorkspaceID: runtimeAccountTestWSID,
			AgentID:     agentID,
			UserID:      runtimeAccountTestUserID,
		},
	}

	// A source in Finance the rule never named passes via folder resolution.
	if err := tool.chainGateDataSource(ctx, "execute", financeOld, srv.URL, "k"); err != nil {
		t.Fatalf("Finance source %q must pass via folder scope, got error: %v", financeOld, err)
	}
	// A NEW source added to Finance is auto-covered without editing the rule.
	if err := tool.chainGateDataSource(ctx, "execute", financeNew, srv.URL, "k"); err != nil {
		t.Fatalf("new Finance source %q must be auto-covered, got error: %v", financeNew, err)
	}
	// A source in another folder is denied — the whitelist closes to Deny.
	if err := tool.chainGateDataSource(ctx, "execute", hrSource, srv.URL, "k"); err == nil {
		t.Fatalf("non-Finance source %q must be DENIED, got allow", hrSource)
	}
}

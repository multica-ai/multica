package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type knowledgeEnv struct {
	t         *testing.T
	pool      *pgxpool.Pool
	q         *db.Queries
	fake      *knowledge.Fake
	svc       *KnowledgeService
	ws        pgtype.UUID
	user      pgtype.UUID
	wsID      string
	userID    string
	liveAgent string
	oldAgent  string
}

func newKnowledgeEnv(t *testing.T) *knowledgeEnv {
	t.Helper()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, "", "")
	userID := fx.User(t, "Knowledge Owner", "knowledge-owner-"+t.Name()+"@example.test")
	fx = testutil.New(pool, "", userID)
	wsID := fx.Workspace(t, "Knowledge WS", "knowledge-"+strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")))
	fx = testutil.New(pool, wsID, userID)
	fx.Member(t, wsID, userID, "owner")
	live := fx.Agent(t, "live", "")
	old := fx.Agent(t, "archived", "", testutil.Cols{"archived_at": testutil.Raw("now()")})

	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	fake := knowledge.NewFake()
	env := &knowledgeEnv{
		t: t, pool: pool, q: db.New(pool), fake: fake, wsID: wsID, userID: userID, liveAgent: live, oldAgent: old,
		ws: mustUUID(t, wsID), user: mustUUID(t, userID),
	}
	env.svc = &KnowledgeService{
		Queries: env.q, DB: pool, Client: fake, Box: box,
		Embedding: knowledge.EmbeddingModelSpec{Name: "bge-m3", Provider: "ollama", BaseURL: "http://ollama.test", Dimension: 1024},
		UIURL:     "http://ui.test",
	}
	// These tables have no foreign keys, so nothing removes them with the
	// workspace. Registered last, this runs before the fixture teardown.
	t.Cleanup(func() {
		ctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM agent_mcp_server WHERE server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_id = $1)`,
			`DELETE FROM workspace_mcp_server WHERE workspace_id = $1`,
			`DELETE FROM knowledge_link WHERE workspace_id = $1`,
			`DELETE FROM knowledge_base WHERE workspace_id = $1`,
			`DELETE FROM workspace_knowledge WHERE workspace_id = $1`,
		} {
			if _, err := pool.Exec(ctx, stmt, wsID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})
	return env
}

func (e *knowledgeEnv) enable() (KnowledgeStatus, error) {
	return e.svc.Enable(context.Background(), e.ws, e.user)
}

func (e *knowledgeEnv) bound(agentID string) bool {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_mcp_server ams JOIN workspace_mcp_server s ON s.id = ams.server_id
		 WHERE ams.agent_id = $1 AND s.name = $2 AND s.workspace_id = $3`, agentID, KnowledgeMCPServerName, e.wsID).Scan(&n)
	if err != nil {
		e.t.Fatal(err)
	}
	return n > 0
}

func (e *knowledgeEnv) libraryEntry() db.WorkspaceMcpServer {
	e.t.Helper()
	entry, err := e.q.GetWorkspaceMcpServerByName(context.Background(), db.GetWorkspaceMcpServerByNameParams{WorkspaceID: e.ws, Name: KnowledgeMCPServerName})
	if err != nil {
		e.t.Fatalf("library entry: %v", err)
	}
	return entry
}

func TestKnowledgeEnableProvisionsEverything(t *testing.T) {
	e := newKnowledgeEnv(t)
	status, err := e.enable()
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !status.Offered || !status.Enabled || status.State != KnowledgeStateReady || status.UIURL != "http://ui.test" {
		t.Fatalf("status = %+v", status)
	}
	var keys []string
	for _, kb := range status.KnowledgeBases {
		keys = append(keys, kb.Key)
	}
	if strings.Join(keys, ",") != "company,market,product,engineering,decisions" {
		t.Fatalf("knowledge bases = %v", keys)
	}

	// The tenant is named after the workspace, and the key is stored sealed.
	tenants := e.fake.Tenants()
	if len(tenants) != 1 || tenants[0].Name != "multica-"+e.wsID {
		t.Fatalf("tenants = %+v", tenants)
	}
	row, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws)
	if err != nil || row.WeknoraTenantID != tenants[0].ID || row.State != "ready" {
		t.Fatalf("row = %+v, %v", row, err)
	}
	if strings.Contains(string(row.ApiKeyCiphertext), "sk-fake") {
		t.Fatal("the workspace key is stored in plaintext")
	}
	plain, err := e.svc.Box.Open(row.ApiKeyCiphertext)
	if err != nil || !strings.HasPrefix(string(plain), "sk-fake-") {
		t.Fatalf("sealed key does not open to the minted token: %q, %v", plain, err)
	}

	// The embedding model was ensured exactly once, before the bases.
	if e.fake.Calls(knowledge.OpEnsureModel) != 1 || e.fake.Calls(knowledge.OpCreateKB) != 5 {
		t.Fatalf("model calls = %d, kb creates = %d", e.fake.Calls(knowledge.OpEnsureModel), e.fake.Calls(knowledge.OpCreateKB))
	}

	// The read-only endpoint is in the library with its token as a bearer header.
	entry := e.libraryEntry()
	var cfg struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(entry.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	ws := e.fake.Workspace(string(plain))
	endpoints, _ := ws.ListMCPEndpoints(context.Background())
	if len(endpoints) != 1 || endpoints[0].Name != "company-read" || len(endpoints[0].KnowledgeBaseIDs) != 5 || len(endpoints[0].Tools) != 8 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	for _, tool := range endpoints[0].Tools {
		if tool == "ask" || strings.HasPrefix(tool, "add_") || strings.HasPrefix(tool, "update_") || strings.HasPrefix(tool, "delete_") {
			t.Fatalf("endpoint exposes %s", tool)
		}
	}
	if cfg.Type != "http" || cfg.URL != e.fake.MCPURL(endpoints[0].ID) || !strings.HasPrefix(cfg.Headers["Authorization"], "Bearer mcp_token-") {
		t.Fatalf("library config = %s", entry.Config)
	}

	// Live agents received it; archived ones did not.
	if !e.bound(e.liveAgent) || e.bound(e.oldAgent) {
		t.Fatalf("assignment: live=%v archived=%v", e.bound(e.liveAgent), e.bound(e.oldAgent))
	}
}

func TestKnowledgeEnableIsIdempotent(t *testing.T) {
	e := newKnowledgeEnv(t)
	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	before := e.libraryEntry()
	status, err := e.enable()
	if err != nil || status.State != KnowledgeStateReady || len(status.KnowledgeBases) != 5 {
		t.Fatalf("second Enable = %+v, %v", status, err)
	}
	for op, want := range map[string]int{
		knowledge.OpEnsureTenant: 1, knowledge.OpCreateWorkspaceKey: 1, knowledge.OpCreateKB: 5,
		knowledge.OpCreateMCPEndpoint: 1, knowledge.OpRotateMCPToken: 0,
	} {
		if got := e.fake.Calls(op); got != want {
			t.Errorf("%s calls = %d, want %d", op, got, want)
		}
	}
	after := e.libraryEntry()
	if after.ID != before.ID || string(after.Config) != string(before.Config) {
		t.Fatal("the library entry (and its token) changed on a repeat Enable")
	}
}

func TestKnowledgeEnableResumesAfterFailureAtEachStep(t *testing.T) {
	for _, step := range []string{
		knowledge.OpEnsureTenant, knowledge.OpCreateWorkspaceKey, knowledge.OpEnsureModel,
		knowledge.OpCreateKB, knowledge.OpListMCPEndpoints, knowledge.OpCreateMCPEndpoint,
	} {
		t.Run(step, func(t *testing.T) {
			e := newKnowledgeEnv(t)
			boom := errors.New("injected")
			e.fake.FailNext(step, boom)
			if _, err := e.enable(); !errors.Is(err, boom) {
				t.Fatalf("first Enable err = %v, want the injected failure", err)
			}
			// Before the tenant row exists there is nothing to mark failed.
			if row, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws); err == nil && row.State != KnowledgeStateFailed {
				t.Fatalf("state after failure = %q, want failed", row.State)
			}
			status, err := e.enable()
			if err != nil || status.State != KnowledgeStateReady || len(status.KnowledgeBases) != 5 {
				t.Fatalf("retry = %+v, %v", status, err)
			}
			// Nothing was created twice.
			if n := len(e.fake.Tenants()); n != 1 {
				t.Fatalf("tenants = %d", n)
			}
			if got := e.fake.Calls(knowledge.OpCreateMCPEndpoint); step != knowledge.OpCreateMCPEndpoint && got != 1 {
				t.Fatalf("endpoint creates = %d", got)
			}
			ws := e.fake.Workspace(e.openKey())
			if kbs, _ := ws.ListKnowledgeBases(context.Background()); len(kbs) != 5 {
				t.Fatalf("remote knowledge bases = %d", len(kbs))
			}
			if eps, _ := ws.ListMCPEndpoints(context.Background()); len(eps) != 1 {
				t.Fatalf("remote endpoints = %d", len(eps))
			}
			if !e.bound(e.liveAgent) {
				t.Fatal("agent not assigned after resume")
			}
		})
	}
}

func (e *knowledgeEnv) openKey() string {
	e.t.Helper()
	row, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws)
	if err != nil {
		e.t.Fatal(err)
	}
	plain, err := e.svc.Box.Open(row.ApiKeyCiphertext)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(plain)
}

func TestKnowledgeEnableAdoptsWhatACrashLeftBehind(t *testing.T) {
	e := newKnowledgeEnv(t)
	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// A crash after WeKnora created a base but before Multica recorded it, and
	// after the endpoint was created but before the library entry was.
	if _, err := e.pool.Exec(ctx, `DELETE FROM knowledge_base WHERE workspace_id = $1 AND key = 'market'`, e.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM agent_mcp_server WHERE server_id IN (SELECT id FROM workspace_mcp_server WHERE workspace_id = $1)`, e.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM workspace_mcp_server WHERE workspace_id = $1`, e.wsID); err != nil {
		t.Fatal(err)
	}
	status, err := e.enable()
	if err != nil || len(status.KnowledgeBases) != 5 {
		t.Fatalf("Enable = %+v, %v", status, err)
	}
	if got := e.fake.Calls(knowledge.OpCreateKB); got != 5 {
		t.Fatalf("knowledge base creates = %d, want the lost base adopted, not recreated", got)
	}
	if got := e.fake.Calls(knowledge.OpCreateMCPEndpoint); got != 1 {
		t.Fatalf("endpoint creates = %d, want the existing endpoint reused", got)
	}
	if got := e.fake.Calls(knowledge.OpRotateMCPToken); got != 1 {
		t.Fatalf("token rotations = %d, want 1 (the lost token cannot be read back)", got)
	}
	if !e.bound(e.liveAgent) {
		t.Fatal("agent not re-assigned")
	}
}

func TestKnowledgeRestrictedBasesStayOutOfCompanyRead(t *testing.T) {
	e := newKnowledgeEnv(t)
	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE knowledge_base SET visibility = 'restricted' WHERE workspace_id = $1 AND key = 'engineering'`, e.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	eps, _ := e.fake.Workspace(e.openKey()).ListMCPEndpoints(context.Background())
	if len(eps) != 1 || len(eps[0].KnowledgeBaseIDs) != 4 {
		t.Fatalf("endpoint bases = %+v", eps)
	}
}

func TestKnowledgeUnavailableSidecarDegrades(t *testing.T) {
	e := newKnowledgeEnv(t)

	// Not enabled: the status is disabled, not an error.
	e.fake.SetUnavailable(true)
	status, err := e.svc.Status(context.Background(), e.ws)
	if err != nil || !status.Offered || status.Enabled || status.State != KnowledgeStateDisabled {
		t.Fatalf("status = %+v, %v", status, err)
	}
	// Enabling needs the sidecar, and says so with the typed error, leaving no row.
	if _, err := e.enable(); !errors.Is(err, knowledge.ErrUnavailable) {
		t.Fatalf("Enable err = %v, want ErrUnavailable", err)
	}
	if _, err := e.q.GetWorkspaceKnowledge(context.Background(), e.ws); err == nil {
		t.Fatal("a row was left behind although no tenant exists")
	}

	// Enabled, then the sidecar goes away: offline, still no error.
	e.fake.SetUnavailable(false)
	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	e.fake.SetUnavailable(true)
	status, err = e.svc.Status(context.Background(), e.ws)
	if err != nil || !status.Enabled || status.State != KnowledgeStateOffline || len(status.KnowledgeBases) != 5 {
		t.Fatalf("offline status = %+v, %v", status, err)
	}
}

func TestKnowledgeNotOffered(t *testing.T) {
	e := newKnowledgeEnv(t)
	cases := map[string]struct {
		svc    *KnowledgeService
		reason string
	}{
		"no sidecar":    {&KnowledgeService{Queries: e.q, DB: e.pool}, "not_configured"},
		"nil service":   {nil, "not_configured"},
		"no secret key": {&KnowledgeService{Queries: e.q, DB: e.pool, Client: e.fake}, "secret_key_missing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			status, err := tc.svc.Status(context.Background(), e.ws)
			if err != nil || status.Offered || status.Reason != tc.reason || status.Enabled || status.State != KnowledgeStateDisabled {
				t.Fatalf("status = %+v, %v", status, err)
			}
			if _, err := tc.svc.Enable(context.Background(), e.ws, e.user); !errors.Is(err, ErrKnowledgeNotOffered) {
				t.Fatalf("Enable err = %v", err)
			}
		})
	}
	if e.fake.Calls(knowledge.OpEnsureTenant) != 0 {
		t.Fatal("a deployment without a secret key still created a tenant")
	}
}

func TestKnowledgeEnableRefusesConcurrentProvisioning(t *testing.T) {
	e := newKnowledgeEnv(t)
	ctx := context.Background()
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 2201))`, e.wsID).Scan(&got); err != nil || !got {
		t.Fatalf("setup lock: %v %v", got, err)
	}
	if _, err := e.enable(); !errors.Is(err, ErrKnowledgeBusy) {
		t.Fatalf("err = %v, want ErrKnowledgeBusy", err)
	}
	if e.fake.Calls(knowledge.OpEnsureTenant) != 0 {
		t.Fatal("provisioned while another request held the lock")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 2201))`, e.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.enable(); err != nil {
		t.Fatalf("Enable after the lock was released: %v", err)
	}
}

func TestKnowledgeNewAgentsReceiveTheServerOnlyWhenReady(t *testing.T) {
	e := newKnowledgeEnv(t)
	ctx := context.Background()
	fx := testutil.New(e.pool, e.wsID, e.userID)

	before := fx.Agent(t, "before-enable", "")
	if err := e.q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: mustUUID(t, before), WorkspaceID: e.ws}); err != nil {
		t.Fatal(err)
	}
	if e.bound(before) {
		t.Fatal("assigned although knowledge is not enabled")
	}

	if _, err := e.enable(); err != nil {
		t.Fatal(err)
	}
	after := fx.Agent(t, "after-enable", "")
	if err := e.q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: mustUUID(t, after), WorkspaceID: e.ws}); err != nil {
		t.Fatal(err)
	}
	if !e.bound(after) {
		t.Fatal("a new agent did not receive company-read")
	}
	// The earlier agent was covered by Enable itself.
	if !e.bound(before) {
		t.Fatal("Enable skipped an agent that existed at the time")
	}

	if _, err := e.pool.Exec(ctx, `UPDATE workspace_knowledge SET state = 'failed' WHERE workspace_id = $1`, e.wsID); err != nil {
		t.Fatal(err)
	}
	late := fx.Agent(t, "while-failed", "")
	if err := e.q.AssignKnowledgeMcpToAgent(ctx, db.AssignKnowledgeMcpToAgentParams{AgentID: mustUUID(t, late), WorkspaceID: e.ws}); err != nil {
		t.Fatal(err)
	}
	if e.bound(late) {
		t.Fatal("assigned while provisioning is not ready")
	}
}

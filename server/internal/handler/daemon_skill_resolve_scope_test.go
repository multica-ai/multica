package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

// The daemon resolves one skill bundle per request (GH #4505), so what the
// resolve endpoint loads per request is multiplied by the agent's skill count
// across a cold dispatch. These tests pin that it loads only the refs it was
// asked for, and that narrowing the read did not narrow what the agent is
// allowed to see -- the junction predicate is now doing the authorization the
// full-set lookup used to do.

// Query markers include the trailing kind so they cannot match a sibling query
// whose name shares a prefix (ListAgentSkills vs ListAgentSkillsByIDs).
const (
	queryFullAgentSkills   = "-- name: ListAgentSkills :many"
	queryScopedAgentSkills = "-- name: ListAgentSkillsByIDs :many"
	querySkillFilesByIDs   = "-- name: ListSkillFilesBySkillIDs :many"
)

var errInjectedScopedSkillRead = errors.New("injected scoped skill read failure")

// skillQuerySpy passes every statement through to the real pool while
// recording which skill reads ran and how many IDs each was given, so a test
// can assert the request cost and not just the response body. failQuery fails
// one chosen query.
type skillQuerySpy struct {
	inner     db.DBTX
	failQuery string

	mu               sync.Mutex
	fullSkillCalls   int
	scopedSkillIDs   []int
	skillFileCallIDs []int
}

func (s *skillQuerySpy) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.inner.Exec(ctx, sql, args...)
}

func (s *skillQuerySpy) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.inner.QueryRow(ctx, sql, args...)
}

func (s *skillQuerySpy) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, queryScopedAgentSkills):
		s.record(&s.scopedSkillIDs, uuidArgLen(args, 1))
		if s.failQuery == queryScopedAgentSkills {
			return nil, errInjectedScopedSkillRead
		}
	case strings.Contains(sql, queryFullAgentSkills):
		s.mu.Lock()
		s.fullSkillCalls++
		s.mu.Unlock()
	case strings.Contains(sql, querySkillFilesByIDs):
		s.record(&s.skillFileCallIDs, uuidArgLen(args, 0))
	}
	return s.inner.Query(ctx, sql, args...)
}

func (s *skillQuerySpy) record(into *[]int, n int) {
	s.mu.Lock()
	*into = append(*into, n)
	s.mu.Unlock()
}

func (s *skillQuerySpy) snapshot() (fullCalls int, scoped, files []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullSkillCalls, append([]int(nil), s.scopedSkillIDs...), append([]int(nil), s.skillFileCallIDs...)
}

func uuidArgLen(args []any, i int) int {
	if i >= len(args) {
		return -1
	}
	ids, ok := args[i].([]pgtype.UUID)
	if !ok {
		return -1
	}
	return len(ids)
}

func newSpyHandler(t *testing.T, spy *skillQuerySpy) *Handler {
	t.Helper()
	return New(db.New(spy), testPool, testHandler.Hub, testHandler.Bus, testHandler.EmailService,
		nil, nil, analytics.NoopClient{}, Config{})
}

// resolveScopeFixture creates a runtime, a dispatched task, and an agent
// carrying one skill per name in skillNames (each with one supporting file).
func resolveScopeFixture(t *testing.T, ctx context.Context, name string, skillNames ...string) (runtimeID, taskID string, skillIDs []string) {
	t.Helper()

	runtimeID = createClaimReclaimRuntime(t, ctx, name+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, name+" agent")

	for _, skillName := range skillNames {
		skillID := dbfx.Insert(t, "skill", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"name":         skillName,
			"description":  "Resolve scope fixture",
			"content":      skillName + " content",
			"config":       testutil.Raw("'{}'::jsonb"),
			"created_by":   testUserID,
		})
		dbfx.InsertNoID(t, "skill_file", testutil.Cols{
			"skill_id": skillID,
			"path":     "rules.md",
			"content":  skillName + " rules",
		}, "skill_id = $1", skillID)
		dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
			"agent_id": agentID,
			"skill_id": skillID,
		}, "skill_id = $1", skillID)
		skillIDs = append(skillIDs, skillID)
	}

	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":    runtimeID,
		"issue_id":      issueID,
		"status":        "dispatched",
		"dispatched_at": testutil.Raw("now()"),
	})
	return runtimeID, taskID, skillIDs
}

func resolveBundles(t *testing.T, h *Handler, runtimeID, taskID string, refs ...resolveSkillBundleRef) *testutil.Response {
	t.Helper()
	req := newDaemonTokenRequest("POST",
		"/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve",
		resolveSkillBundlesRequest{Skills: refs}, testWorkspaceID, "resolve-scope-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	return testutil.Call(t, h.ResolveTaskSkillBundles, req)
}

func workspaceRef(skillID string) resolveSkillBundleRef {
	return resolveSkillBundleRef{ID: skillID, Source: skillbundle.SourceWorkspace, Hash: "sha256:whatever"}
}

// TestResolveTaskSkillBundles_LoadsOnlyTheRequestedSkill is the point of the
// change: an agent with three skills asks for one, and the server must read
// and hash one. Before, every resolve request loaded the agent's entire skill
// set to pick a single bundle out of it, so a cold dispatch of N skills cost N
// full loads.
func TestResolveTaskSkillBundles_LoadsOnlyTheRequestedSkill(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	spy := &skillQuerySpy{inner: testPool}
	runtimeID, taskID, skillIDs := resolveScopeFixture(t, ctx, "resolvescope",
		"resolvescope-alpha", "resolvescope-beta", "resolvescope-gamma")

	var resp struct {
		Bundles []service.AgentSkillData `json:"bundles"`
	}
	resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID, workspaceRef(skillIDs[1])).
		Want(http.StatusOK).JSON(&resp)

	if len(resp.Bundles) != 1 || resp.Bundles[0].ID != skillIDs[1] {
		t.Fatalf("resolved %+v, want exactly the requested skill %s", resp.Bundles, skillIDs[1])
	}
	if len(resp.Bundles[0].Files) != 1 || resp.Bundles[0].Files[0].Content != "resolvescope-beta rules" {
		t.Fatalf("resolved bundle lost its files: %+v", resp.Bundles[0].Files)
	}
	if resp.Bundles[0].Hash == "" {
		t.Fatal("resolved bundle has no hash")
	}

	fullCalls, scoped, files := spy.snapshot()
	if fullCalls != 0 {
		t.Fatalf("the full ListAgentSkills ran %d times; resolve must not load the agent's whole skill set", fullCalls)
	}
	if len(scoped) != 1 || scoped[0] != 1 {
		t.Fatalf("scoped skill query calls = %v, want exactly one call for one id", scoped)
	}
	if len(files) != 1 || files[0] != 1 {
		t.Fatalf("skill file query calls = %v, want exactly one call for one id", files)
	}
}

// TestResolveTaskSkillBundles_BuiltinRefTouchesNoWorkspaceQuery pins the other
// half: built-ins live in the binary, so resolving one must not reach the
// database at all.
func TestResolveTaskSkillBundles_BuiltinRefTouchesNoWorkspaceQuery(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	spy := &skillQuerySpy{inner: testPool}
	runtimeID, taskID, _ := resolveScopeFixture(t, ctx, "resolvebuiltin", "resolvebuiltin-alpha")

	// The unscoped set: resolution deliberately serves any built-in by id.
	builtins := testHandler.TaskService.AllBuiltinSkills()
	if len(builtins) == 0 {
		t.Skip("no builtin skills embedded in this build")
	}
	want := builtins[0]

	var resp struct {
		Bundles []service.AgentSkillData `json:"bundles"`
	}
	resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID, resolveSkillBundleRef{
		ID:     service.BuiltinSkillID(want.Name),
		Source: skillbundle.SourceBuiltin,
		Hash:   "sha256:whatever",
	}).Want(http.StatusOK).JSON(&resp)

	if len(resp.Bundles) != 1 || resp.Bundles[0].Name != want.Name || resp.Bundles[0].Content != want.Content {
		t.Fatalf("resolved %+v, want builtin %q", resp.Bundles, want.Name)
	}

	fullCalls, scoped, files := spy.snapshot()
	if fullCalls != 0 || len(scoped) != 0 || len(files) != 0 {
		t.Fatalf("builtin resolve hit the database: full=%d scoped=%v files=%v", fullCalls, scoped, files)
	}
}

// TestResolveTaskSkillBundles_KeepsRequestOrderAndDeduplicates: the scoped
// query orders by name, so response order can no longer fall out of the load.
// It must follow the request, and a repeated ref must not be loaded twice.
func TestResolveTaskSkillBundles_KeepsRequestOrderAndDeduplicates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	spy := &skillQuerySpy{inner: testPool}
	// Named so the query's name ASC order is the reverse of the request order.
	runtimeID, taskID, skillIDs := resolveScopeFixture(t, ctx, "resolveorder",
		"resolveorder-zulu", "resolveorder-alpha")
	zulu, alpha := skillIDs[0], skillIDs[1]

	var resp struct {
		Bundles []service.AgentSkillData `json:"bundles"`
	}
	resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID,
		workspaceRef(zulu), workspaceRef(alpha), workspaceRef(zulu)).
		Want(http.StatusOK).JSON(&resp)

	got := []string{}
	for _, bundle := range resp.Bundles {
		got = append(got, bundle.ID)
	}
	want := []string{zulu, alpha, zulu}
	if len(got) != len(want) {
		t.Fatalf("resolved %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved %v, want request order %v", got, want)
		}
	}

	_, scoped, files := spy.snapshot()
	if len(scoped) != 1 || scoped[0] != 2 {
		t.Fatalf("scoped skill query calls = %v, want one call for 2 distinct ids (the repeated ref must not load twice)", scoped)
	}
	if len(files) != 1 || files[0] != 2 {
		t.Fatalf("skill file query calls = %v, want one call for 2 ids", files)
	}
}

// TestResolveTaskSkillBundles_RefusesSkillsTheAgentCannotSee is the reason the
// narrowed read is safe: the old code authorized by membership in the agent's
// full bundle set, and the new query has to reject exactly the same refs.
func TestResolveTaskSkillBundles_RefusesSkillsTheAgentCannotSee(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID, taskID, skillIDs := resolveScopeFixture(t, ctx, "resolvedeny", "resolvedeny-owned")
	agentID := ""
	dbfx.QueryRow(t, `SELECT agent_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&agentID)

	// A workspace skill nobody assigned to this agent.
	unassigned := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "resolvedeny-unassigned",
		"description":  "",
		"content":      "unassigned content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})

	// Assigned, but the assignment is disabled.
	disabled := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "resolvedeny-disabled",
		"description":  "",
		"content":      "disabled content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": agentID,
		"skill_id": disabled,
		"enabled":  false,
	}, "skill_id = $1", disabled)

	// A skill owned by a different workspace.
	foreignWorkspace := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Resolve deny foreign",
		"slug":         "resolve-deny-foreign",
		"description":  "",
		"issue_prefix": "RDF",
	})
	foreign := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": foreignWorkspace,
		"name":         "resolvedeny-foreign",
		"description":  "",
		"content":      "foreign content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})

	tests := []struct {
		name string
		ref  resolveSkillBundleRef
	}{
		{name: "skill not assigned to the agent", ref: workspaceRef(unassigned)},
		{name: "assignment disabled", ref: workspaceRef(disabled)},
		{name: "skill owned by another workspace", ref: workspaceRef(foreign)},
		{name: "unparseable skill id", ref: workspaceRef("not-a-uuid")},
		{name: "source with no server-side producer", ref: resolveSkillBundleRef{
			ID: skillIDs[0], Source: skillbundle.SourcePlugin, Hash: "sha256:whatever",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := &skillQuerySpy{inner: testPool}
			resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID, tc.ref).Want(http.StatusNotFound)
		})
	}

	// The agent's own skill still resolves, so the cases above are denials and
	// not a fixture that could never resolve anything.
	spy := &skillQuerySpy{inner: testPool}
	resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID, workspaceRef(skillIDs[0])).Want(http.StatusOK)
}

// TestResolveTaskSkillBundles_ScopedReadFailureReturns500 extends the
// fail-closed rule to the new query: a failed read must not answer 404, which
// the daemon would treat as a permanent "this skill is gone" rather than
// retrying.
func TestResolveTaskSkillBundles_ScopedReadFailureReturns500(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID, taskID, skillIDs := resolveScopeFixture(t, ctx, "resolvereadfail", "resolvereadfail-alpha")

	spy := &skillQuerySpy{inner: testPool, failQuery: queryScopedAgentSkills}
	resolveBundles(t, newSpyHandler(t, spy), runtimeID, taskID, workspaceRef(skillIDs[0])).
		Want(http.StatusInternalServerError)
}

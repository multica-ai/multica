package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inject a statement failure inside a real transaction, without changing
// shared database objects or affecting concurrently running test packages.
type lifecycleFailureStarter struct {
	txStarter
	statement string
}
type lifecycleFailureTx struct {
	pgx.Tx
	statement string
}
type lifecycleErrorRow struct{}

func (lifecycleErrorRow) Scan(...any) error { return errors.New("injected lifecycle failure") }
func (s lifecycleFailureStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return lifecycleFailureTx{Tx: tx, statement: s.statement}, nil
}
func (tx lifecycleFailureTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, tx.statement) {
		return lifecycleErrorRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func lifecycleRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/agents", h.CreateAgent)
	r.Put("/api/agents/{id}", h.UpdateAgent)
	r.Post("/api/agents/{id}/archive", h.ArchiveAgent)
	r.Post("/api/agents/{id}/restore", h.RestoreAgent)
	return r
}

func lifecycleCreate(t *testing.T, h *Handler, runtimeID, name string) AgentResponse {
	t.Helper()
	var a AgentResponse
	testutil.Call(t, lifecycleRouter(h).ServeHTTP, newRequest("POST", "/api/agents", map[string]any{
		"name": name, "runtime_id": runtimeID, "runtime_config": map[string]string{"mode": "native-text"},
	})).Want(http.StatusCreated).JSON(&a)
	dbfx.Cleanup(t, `DELETE FROM agent WHERE id=$1`, a.ID)
	dbfx.Cleanup(t, `DELETE FROM kagent_enrollment WHERE agent_id=$1`, a.ID)
	return a
}

func lifecycleEnrollment(t *testing.T, a AgentResponse) db.KagentEnrollment {
	t.Helper()
	e, err := testHandler.Queries.GetKagentEnrollment(t.Context(), db.GetKagentEnrollmentParams{
		AgentID: parseUUID(a.ID), RuntimeID: parseUUID(a.RuntimeID), WorkspaceID: parseUUID(a.WorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestKagentLifecycleAtomicCreateAndUpdate(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	runtimeID := dbfx.Runtime(t, "Lifecycle atomic", testutil.Cols{"provider": "kagent"})
	a := lifecycleCreate(t, testHandler, runtimeID, "Lifecycle atomic")
	e := lifecycleEnrollment(t, a)
	if e.Revision != 1 || e.Generation != 1 || e.DesiredState != "present" || e.Phase != "pending" {
		t.Fatalf("initial enrollment: %+v", e)
	}
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID,
		map[string]any{"instructions": "new instructions", "name": "Lifecycle edited"})).Want(http.StatusOK)
	e = lifecycleEnrollment(t, a)
	if e.Revision != 2 || e.Generation != 2 || e.DesiredState != "present" || e.Phase != "pending" {
		t.Fatalf("updated enrollment: %+v", e)
	}
	var instructions string
	dbfx.QueryRow(t, `SELECT instructions FROM agent WHERE id=$1`, a.ID).Scan(&instructions)
	if instructions != "new instructions" {
		t.Fatalf("instructions=%q", instructions)
	}
	// Invalid edits and a disabled feature cannot advance intent or save metadata.
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"model": "unsupported"})).Want(http.StatusBadRequest)
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"instructions": "disabled"})).Want(http.StatusBadRequest)
	if got := lifecycleEnrollment(t, a); got.Generation != 2 || got.Revision != 2 {
		t.Fatalf("rejected edits advanced generation: %+v", got)
	}
}

func TestKagentLifecycleRollback(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	runtimeID := dbfx.Runtime(t, "Lifecycle rollback", testutil.Cols{"provider": "kagent"})
	for _, commitFailure := range []bool{false, true} {
		h := *testHandler
		name := "Lifecycle failed statement"
		h.TxStarter = lifecycleFailureStarter{txStarter: testHandler.TxStarter, statement: "INSERT INTO kagent_enrollment"}
		if commitFailure {
			h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
			name = "Lifecycle failed commit"
		}
		testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("POST", "/api/agents", map[string]any{
			"name": name, "runtime_id": runtimeID, "runtime_config": map[string]string{"mode": "native-text"},
		})).Want(http.StatusInternalServerError)
		var count int
		dbfx.QueryRow(t, `SELECT count(*) FROM agent WHERE workspace_id=$1 AND name=$2`, testWorkspaceID, name).Scan(&count)
		if count != 0 {
			t.Fatal("failed create persisted agent")
		}
		dbfx.QueryRow(t, `SELECT count(*) FROM kagent_enrollment WHERE runtime_id=$1`, runtimeID).Scan(&count)
		if count != 0 {
			t.Fatal("failed create persisted enrollment")
		}
	}
	a := lifecycleCreate(t, testHandler, runtimeID, "Lifecycle rollback update")
	for _, statement := range []string{"UPDATE kagent_enrollment", "SELECT agent_id FROM kagent_enrollment", "-- name: ClearAgentMcpConfig"} {
		h := *testHandler
		h.TxStarter = lifecycleFailureStarter{txStarter: testHandler.TxStarter, statement: statement}
		testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"name": "must roll back", "instructions": "must roll back", "mcp_config": nil})).Want(http.StatusInternalServerError)
		var name, instructions string
		dbfx.QueryRow(t, `SELECT name,instructions FROM agent WHERE id=$1`, a.ID).Scan(&name, &instructions)
		if name != a.Name || instructions != "" || lifecycleEnrollment(t, a).Generation != 1 {
			t.Fatal("failed update partially committed")
		}
	}
	h := *testHandler
	h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"name": "failed commit"})).Want(http.StatusInternalServerError)
	if lifecycleEnrollment(t, a).Generation != 1 {
		t.Fatal("failed commit advanced enrollment")
	}
	// Missing or absent intent must fail closed, never implicitly recreate it.
	dbfx.Exec(t, `UPDATE kagent_enrollment SET desired_state='absent',phase='draining' WHERE agent_id=$1`, a.ID)
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"name": "absent"})).Want(http.StatusConflict)
	dbfx.Exec(t, `DELETE FROM kagent_enrollment WHERE agent_id=$1`, a.ID)
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"name": "missing"})).Want(http.StatusInternalServerError)
}

func TestKagentLifecycleUnsafeOperationsAndProviderRegression(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	runtimeID := dbfx.Runtime(t, "Lifecycle safety", testutil.Cols{"provider": "kagent"})
	otherRuntime := dbfx.Runtime(t, "Lifecycle ordinary", testutil.Cols{"provider": "claude"})
	a := lifecycleCreate(t, testHandler, runtimeID, "Lifecycle safety")
	other := dbfx.Agent(t, "Lifecycle ordinary", otherRuntime)
	router := lifecycleRouter(testHandler)
	for _, body := range []map[string]any{
		{"runtime_id": otherRuntime},
		{"runtime_id": dbfx.Runtime(t, "Lifecycle second", testutil.Cols{"provider": "kagent"}), "runtime_config": map[string]string{"mode": "native-text"}},
	} {
		testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, body)).Want(http.StatusConflict)
	}
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+other, map[string]any{"runtime_id": runtimeID, "runtime_config": map[string]string{"mode": "native-text"}})).Want(http.StatusConflict)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents/"+a.ID+"/archive", map[string]bool{"remote_quiescence_verified": true, "remote_deletion_verified": true})).Want(http.StatusConflict)
	dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, a.ID)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents/"+a.ID+"/restore", nil)).Want(http.StatusConflict)
	// Enrollment must still fence the agent after runtime unbinding.
	dbfx.Exec(t, `UPDATE agent SET runtime_id=NULL,archived_at=NULL WHERE id=$1`, a.ID)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents/"+a.ID+"/archive", nil)).Want(http.StatusConflict)
	if lifecycleEnrollment(t, a).Generation != 1 {
		t.Fatal("unsafe operations changed intent")
	}
	testutil.Call(t, router.ServeHTTP, newRequest("PUT", "/api/agents/"+other, map[string]any{"instructions": "ordinary edit"})).Want(http.StatusOK)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents/"+other+"/archive", nil)).Want(http.StatusOK)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/agents/"+other+"/restore", nil)).Want(http.StatusOK)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM kagent_enrollment WHERE agent_id=$1`, other).Scan(&count)
	if count != 0 {
		t.Fatal("ordinary provider enrolled")
	}
}

func TestKagentLifecycleRejectsStaleValidationAndScope(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	runtimeID := dbfx.Runtime(t, "Lifecycle scope", testutil.Cols{"provider": "kagent"})
	a := lifecycleCreate(t, testHandler, runtimeID, "Lifecycle scope")
	// A second committed editor changes the snapshot after preflight validation.
	h := *testHandler
	h.TxStarter = hookedTxStarter{base: testHandler.TxStarter, hook: func() error {
		_, err := testPool.Exec(t.Context(), `UPDATE agent SET instructions='concurrent',updated_at=clock_timestamp() WHERE id=$1`, a.ID)
		return err
	}}
	testutil.Call(t, lifecycleRouter(&h).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"instructions": "stale"})).Want(http.StatusConflict)
	var instructions string
	dbfx.QueryRow(t, `SELECT instructions FROM agent WHERE id=$1`, a.ID).Scan(&instructions)
	if instructions != "concurrent" || lifecycleEnrollment(t, a).Generation != 1 {
		t.Fatal("stale validation overwrote committed state")
	}
	// Scope mismatch is a persistence failure, never an implicit enrollment move.
	otherRuntime := dbfx.Runtime(t, "Lifecycle wrong scope", testutil.Cols{"provider": "kagent"})
	dbfx.Exec(t, `UPDATE kagent_enrollment SET runtime_id=$2 WHERE agent_id=$1`, a.ID, otherRuntime)
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"instructions": "wrong scope"})).Want(http.StatusInternalServerError)
	dbfx.QueryRow(t, `SELECT instructions FROM agent WHERE id=$1`, a.ID).Scan(&instructions)
	if instructions != "concurrent" {
		t.Fatal("scope mismatch saved agent")
	}
	// Existing authenticated loaders reject an unknown user before lifecycle writes.
	req := newRequest("PUT", "/api/agents/"+a.ID, map[string]any{"instructions": "unauthorized"})
	req.Header.Set("X-User-ID", "00000000-0000-0000-0000-000000000099")
	testutil.Call(t, lifecycleRouter(testHandler).ServeHTTP, req).Want(http.StatusNotFound)
}

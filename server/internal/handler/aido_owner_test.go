package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Exercise both transaction orders with independent connections. A timeout
// bounds failures; pg_blocking_pids proves the second operation actually waits.
func TestAIDOwnerConcurrentBindingMint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, bindingFirst := range []bool{true, false} {
		t.Run(fmt.Sprint("binding-first=", bindingFirst), func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "scope-race", []byte("[]"))
			runtimeID := handlerTestRuntimeID(t)
			var taskID string
			if err := testPool.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,status) VALUES($1,$2,'running') RETURNING id`, agentID, runtimeID).Scan(&taskID); err != nil {
				t.Fatal(err)
			}
			raw, _ := auth.GenerateAgentTaskToken()
			a, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Rollback(ctx)
			b, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Rollback(ctx)
			var pid int
			if err := b.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			bind := func(q *db.Queries) error {
				_, err := q.CreateAIDOwnerBinding(ctx, db.CreateAIDOwnerBindingParams{AgentID: parseUUID(agentID), RuntimeID: parseUUID(runtimeID)})
				return err
			}
			mint := func(q *db.Queries) error {
				_, err := q.CreateTaskToken(ctx, db.CreateTaskTokenParams{TokenHash: auth.HashToken(raw), TaskID: parseUUID(taskID), AgentID: parseUUID(agentID), WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
				return err
			}
			first, second := bind, mint
			if !bindingFirst {
				first, second = mint, bind
			}
			if err := first(testHandler.Queries.WithTx(a)); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- second(testHandler.Queries.WithTx(b)) }()
			blocked := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if err := testPool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked); err != nil {
					break
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Always release and collect the concurrent operation before reporting.
			commitErr := a.Commit(ctx)
			secondErr := <-done
			if !blocked {
				t.Error("second operation escaped transaction serialization")
			}
			if commitErr != nil || secondErr != nil {
				t.Fatal(commitErr, secondErr)
			}
			if err := b.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			token, err := testHandler.Queries.GetTaskTokenByHash(ctx, auth.HashToken(raw))
			if err != nil || !token.IsAidoOwner {
				t.Fatal("raced token retained broad scope", err)
			}
			req := httptest.NewRequest("GET", "/api/issues", nil)
			req.Header.Set("Authorization", "Bearer "+raw)
			w := httptest.NewRecorder()
			middleware.Auth(testHandler.Queries, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatal("raced token escaped", w.Code)
			}
			t.Log("second transaction blocked; committed token scoped=true; GET /api/issues=403")
			stale, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = stale.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
			if err == nil {
				err = mint(testHandler.Queries.WithTx(stale))
			}
			_ = stale.Rollback(ctx)
			if err == nil || !strings.Contains(err.Error(), "READ COMMITTED") {
				t.Fatal("stale-snapshot mint did not fail closed", err)
			}
		})
	}
}

func TestAIDOwnerTaskCapabilityBoundary(t *testing.T) {
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "isolated-owner", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	session := func() string {
		request := withChatTestWorkspaceCtx(t, newRequest("POST", "/api/chat/sessions", map[string]string{"agent_id": agentID}))
		recorder := httptest.NewRecorder()
		testHandler.CreateChatSession(recorder, request)
		if recorder.Code != 201 {
			t.Fatal(recorder.Code, recorder.Body.String())
		}
		var result ChatSessionResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.ID
	}
	ownSession, otherSession := session(), session()
	task := func(agentID, sessionID string) string {
		var id string
		if err := testPool.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,chat_session_id,status) VALUES($1,$2,$3,'running') RETURNING id`, agentID, runtimeID, sessionID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ownTask := task(agentID, ownSession)
	mint := func(agentID, taskID string) string {
		raw, err := auth.GenerateAgentTaskToken()
		if err != nil {
			t.Fatal(err)
		}
		_, err = testHandler.Queries.CreateTaskToken(ctx, db.CreateTaskTokenParams{
			TokenHash: auth.HashToken(raw), TaskID: parseUUID(taskID), AgentID: parseUUID(agentID),
			WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	scopedToken := mint(agentID, ownTask) // Existing tokens must narrow on binding too.
	invoke := func(raw, method, path string, body any, handle http.HandlerFunc) *httptest.ResponseRecorder {
		request := withChatTestWorkspaceCtx(t, newRequest(method, path, body))
		request = withURLParam(request, "id", agentID)
		request.Header.Set("Authorization", "Bearer "+raw)
		request.Header.Set("X-Actor-Source", "owner_direct")
		recorder := httptest.NewRecorder()
		middleware.Auth(testHandler.Queries, nil, nil)(handle).ServeHTTP(recorder, request)
		return recorder
	}
	ownerRaw, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	pat, err := testHandler.Queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID: parseUUID(testUserID), Name: "binding-test", TokenHash: auth.HashToken(ownerRaw), TokenPrefix: ownerRaw[:12],
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM personal_access_token WHERE id=$1`, pat.ID) })
	bindPath := "/api/agents/" + agentID + "/aido-owner-binding"
	if w := invoke(scopedToken, "POST", bindPath, map[string]string{"runtime_id": runtimeID}, testHandler.BindAIDOwner); w.Code != 403 {
		t.Fatal("machine bound", w.Code)
	}
	if w := invoke(ownerRaw, "POST", bindPath, map[string]string{"runtime_id": runtimeID}, testHandler.BindAIDOwner); w.Code != 201 {
		t.Fatal("bind", w.Code, w.Body.String())
	}
	if w := invoke(ownerRaw, "GET", bindPath, nil, testHandler.GetAIDOwnerBinding); w.Code != 200 {
		t.Fatal("binding introspection", w.Code)
	}
	otherRuntime := createHandlerTestRuntimeWithProvider(t, "codex")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, agentID, otherRuntime); err == nil {
		t.Fatal("runtime boundary moved")
	}
	if _, err := testPool.Exec(ctx, `UPDATE aido_owner_binding SET runtime_id=$2 WHERE agent_id=$1`, agentID, otherRuntime); err == nil {
		t.Fatal("immutable binding changed")
	}
	token, err := testHandler.Queries.GetTaskTokenByHash(ctx, auth.HashToken(scopedToken))
	if err != nil || !token.IsAidoOwner {
		t.Fatal("existing token did not narrow", err)
	}
	fresh := mint(agentID, ownTask)
	newlyMinted, err := testHandler.Queries.GetTaskTokenByHash(ctx, auth.HashToken(fresh))
	if err != nil || !newlyMinted.IsAidoOwner {
		t.Fatal("new token did not narrow", err)
	}
	attachment := func(sessionID string) string {
		var id string
		err := testPool.QueryRow(ctx, `INSERT INTO attachment(workspace_id,chat_session_id,uploader_type,uploader_id,filename,url,content_type,size_bytes) VALUES($1,$2,'member',$3,'synthetic.txt','synthetic','text/plain',1) RETURNING id`, testWorkspaceID, sessionID, testUserID).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ownAttachment, otherAttachment := attachment(ownSession), attachment(otherSession)
	pass := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	// Simulate a legacy token left broad by migration 135's mint race.
	// Only this isolated fixture transaction bypasses the sticky-scope trigger.
	legacyTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer legacyTx.Rollback(ctx)
	if _, err := legacyTx.Exec(ctx, `ALTER TABLE task_token DISABLE TRIGGER aido_token_scope_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyTx.Exec(ctx, `UPDATE task_token SET is_aido_owner=false WHERE token_hash=$1`, auth.HashToken(scopedToken)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyTx.Exec(ctx, `ALTER TABLE task_token ENABLE TRIGGER aido_token_scope_guard`); err != nil {
		t.Fatal(err)
	}
	if err := legacyTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if w := invoke(scopedToken, "GET", "/api/issues", nil, pass); w.Code != 403 {
		t.Fatal("legacy false scope escaped current binding", w.Code)
	}
	for _, path := range []string{"/api/tasks/" + ownTask + "/messages", "/api/attachments/" + ownAttachment + "/download"} {
		if w := invoke(scopedToken, "GET", path, nil, pass); w.Code != 200 {
			t.Fatalf("own read %s %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/issues", "/api/chat/sessions", "/api/chat/sessions/" + otherSession + "/messages", "/api/agents", "/api/attachments/" + otherAttachment + "/download", "/api/personal-access-tokens"} {
		if w := invoke(scopedToken, "GET", path, nil, pass); w.Code != 403 {
			t.Fatalf("escaped %s %d", path, w.Code)
		}
	}
	if w := invoke(scopedToken, "POST", "/api/issues", nil, pass); w.Code != 403 {
		t.Fatal("write escaped")
	}
	internalAgent := createHandlerTestAgent(t, "ordinary-agent", []byte("[]"))
	internalTask := task(internalAgent, otherSession)
	if w := invoke(mint(internalAgent, internalTask), "GET", "/api/issues", nil, pass); w.Code != 200 {
		t.Fatal("internal regression", w.Code)
	}
	if _, err := testPool.Exec(ctx, `UPDATE task_token SET is_aido_owner=false WHERE token_hash=$1`, auth.HashToken(scopedToken)); err != nil {
		t.Fatal(err)
	}
	token, err = testHandler.Queries.GetTaskTokenByHash(ctx, auth.HashToken(scopedToken))
	if err != nil || !token.IsAidoOwner {
		t.Fatal("scope downgrade")
	}
	migration, err := os.ReadFile("../../migrations/135_aido_owner_scope.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, rollbackErr := tx.Exec(ctx, string(migration))
	_ = tx.Rollback(ctx)
	if rollbackErr == nil || !strings.Contains(rollbackErr.Error(), "reconcile AIDO") {
		t.Fatal("rollback widened access", rollbackErr)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM aido_owner_binding WHERE agent_id=$1`, agentID); err == nil {
		t.Fatal("binding removal must not widen future token mints")
	}
	if w := invoke(scopedToken, "GET", "/api/issues", nil, pass); w.Code != 403 {
		t.Fatal("binding removal widened token", w.Code)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM agent WHERE id=$1`, agentID); err != nil {
		t.Fatal("agent retirement blocked", err)
	}
	if _, err := testHandler.Queries.GetTaskTokenByHash(ctx, auth.HashToken(scopedToken)); err == nil {
		t.Fatal("retired agent token survived")
	}
	t.Log("scoped task: own task+attachment allowed; foreign content/write denied; ordinary task unchanged; sticky scope+rollback guard PASS")
}

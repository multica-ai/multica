package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Each case gets its own workspace so Mika's one-per-workspace lookup cannot
// turn a binding check into the existing-agent fast path.
func runtimeBindingVisibilityRequest(t *testing.T, h *Handler, entry string) (*testutil.Fixture, string, http.HandlerFunc, *http.Request) {
	t.Helper()
	workspaceID := dbfx.Workspace(t, "Runtime binding visibility", "runtime-binding-"+uuid.NewString())
	ownerID := dbfx.User(t, "Runtime binding owner", uuid.NewString()+"@multica.test")
	memberID := dbfx.User(t, "Runtime binding member", uuid.NewString()+"@multica.test")
	dbfx.Member(t, workspaceID, ownerID, "member")
	dbfx.Member(t, workspaceID, memberID, "member")
	fx := testutil.New(testPool, workspaceID, memberID)
	runtimeID := fx.Runtime(t, "Shared binding runtime", testutil.Cols{
		"owner_id": ownerID, "visibility": "public",
	})
	method, path, handler := http.MethodPost, "/api/agent-builder/sessions", h.CreateAgentBuilderSession
	body := map[string]any{"runtime_id": runtimeID}
	sessionID := ""
	agentID := ""
	switch entry {
	case "user create":
		path, handler = "/api/agents", h.CreateAgent
		body["name"] = "Runtime binding agent"
	case "user rebind":
		agentID = fx.Agent(t, "Unbound user agent", "")
		method, path, handler = http.MethodPatch, "/api/agents/"+agentID, h.UpdateAgent
	case "builder create":
	case "builder switch":
		builderID := fx.Agent(t, "Unbound builder", "", testutil.Cols{
			"kind": "system", "system_key": agentBuilderSystemKeyPrefix + uuid.NewString(),
		})
		sessionID = fx.ChatSession(t, builderID)
		method, path, handler = http.MethodPatch, "/api/agent-builder/sessions/"+sessionID+"/runtime", h.SwitchAgentBuilderRuntime
	case "mika create":
		path, handler = "/api/agents/mika", h.CreateMikaAgent
		body["language"] = "en"
	default:
		t.Fatalf("unknown binding entry %q", entry)
	}
	req := newRequestAs(memberID, method, path, body)
	req.Header.Set("X-Workspace-ID", workspaceID)
	member, err := h.Queries.GetMemberByUserAndWorkspace(req.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(memberID), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("load binding member: %v", err)
	}
	req = req.WithContext(middleware.SetMemberContext(req.Context(), workspaceID, member))
	if sessionID != "" {
		req = testutil.WithURLParams(req, "sessionId", sessionID)
	}
	if agentID != "" {
		req = testutil.WithURLParams(req, "id", agentID)
	}
	return fx, runtimeID, handler, req
}

func TestSystemAgentBindingRechecksRuntimeVisibilityInTransaction(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, entry := range []string{"builder create", "builder switch", "mika create"} {
		t.Run(entry, func(t *testing.T) {
			h := *testHandler
			fx, runtimeID, handler, req := runtimeBindingVisibilityRequest(t, &h, entry)
			agentsBefore := fx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`, fx.WorkspaceID)
			sessionsBefore := fx.Count(t, `SELECT count(*) FROM chat_session WHERE workspace_id = $1`, fx.WorkspaceID)
			began := false
			h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
				began = true
				fx.Exec(t, `UPDATE agent_runtime SET visibility = 'private' WHERE id = $1`, runtimeID)
				return testPool.Begin(ctx)
			})

			testutil.Call(t, handler, req).Want(http.StatusForbidden)
			if !began {
				t.Fatal("request did not reach the transaction after public-runtime preflight")
			}
			if got := fx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id = $1`, runtimeID); got != 0 {
				t.Fatalf("private runtime received %d new bindings", got)
			}
			if got := fx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`, fx.WorkspaceID); got != agentsBefore {
				t.Fatalf("rejected binding changed agent count from %d to %d", agentsBefore, got)
			}
			if got := fx.Count(t, `SELECT count(*) FROM chat_session WHERE workspace_id = $1`, fx.WorkspaceID); got != sessionsBefore {
				t.Fatalf("rejected binding changed session count from %d to %d", sessionsBefore, got)
			}
		})
	}
}

func TestSystemAgentBindingWaitsForRuntimeAccessChange(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, entry := range []string{"builder create", "builder switch", "mika create"} {
		t.Run(entry, func(t *testing.T) {
			h := *testHandler
			fx, runtimeID, handler, req := runtimeBindingVisibilityRequest(t, &h, entry)
			ctx, cancel := context.WithTimeout(req.Context(), 20*time.Second)
			defer cancel()
			req = req.WithContext(ctx)
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin runtime holder: %v", err)
			}
			defer tx.Rollback(context.Background())
			if _, err := db.New(tx).LockAgentRuntimeForAccessChange(ctx, parseUUID(runtimeID)); err != nil {
				t.Fatalf("lock runtime for access change: %v", err)
			}
			holderPID := holderBackendPID(t, ctx, tx)
			responses := make(chan *testutil.Response, 1)
			go func() { responses <- testutil.Call(t, handler, req) }()

			if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
				select {
				case response := <-responses:
					t.Fatalf("binding returned %d while the runtime access lock was held: %s", response.Code, response.Text())
				default:
					t.Fatal("binding did not wait for the runtime access lock")
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE agent_runtime SET visibility = 'private' WHERE id = $1`, runtimeID); err != nil {
				t.Fatalf("make locked runtime private: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit runtime access change: %v", err)
			}
			select {
			case response := <-responses:
				response.Want(http.StatusForbidden)
			case <-ctx.Done():
				t.Fatal("binding did not finish after the runtime access lock was released")
			}
			if got := fx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id = $1`, runtimeID); got != 0 {
				t.Fatalf("binding survived committed revocation: count=%d", got)
			}
		})
	}
}

func TestAgentBuilderBindingRechecksRuntimeOnlineInTransaction(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, entry := range []string{"builder create", "builder switch"} {
		t.Run(entry, func(t *testing.T) {
			h := *testHandler
			fx, runtimeID, handler, req := runtimeBindingVisibilityRequest(t, &h, entry)
			h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
				fx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, runtimeID)
				return testPool.Begin(ctx)
			})
			testutil.Call(t, handler, req).Want(http.StatusConflict)
			if got := fx.Count(t, `SELECT count(*) FROM agent WHERE runtime_id = $1`, runtimeID); got != 0 {
				t.Fatalf("offline runtime received %d new bindings", got)
			}
		})
	}
}

func TestMikaExistingAgentRecheckDoesNotRequireRuntimeAccess(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	h := *testHandler
	fx, runtimeID, _, req := runtimeBindingVisibilityRequest(t, &h, "mika create")
	existingID := ""
	h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
		// Another provisioning request wins after this request's fast lookup.
		existingID = fx.Agent(t, service.MikaDefaultName, "", testutil.Cols{
			"system_key": service.MikaSystemKey,
		})
		fx.Exec(t, `UPDATE agent_runtime SET visibility = 'private' WHERE id = $1`, runtimeID)
		return testPool.Begin(ctx)
	})
	var response struct {
		AgentID string `json:"agent_id"`
		Created bool   `json:"created"`
	}
	testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
		agent, created, ok := h.resolveMikaAgent(w, r, fx.WorkspaceID, fx.UserID, createMikaAgentRequest{
			RuntimeID: runtimeID, Language: "en",
		})
		if ok {
			writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "created": created})
		}
	}, req).Want(http.StatusOK).JSON(&response)
	if response.AgentID != existingID || response.Created {
		t.Fatalf("existing Mika was not reused: response=%+v existing=%q", response, existingID)
	}
	if got := fx.Count(t, `SELECT count(*) FROM agent WHERE workspace_id = $1`, fx.WorkspaceID); got != 1 {
		t.Fatalf("idempotent Mika request left %d agents, want 1", got)
	}
}

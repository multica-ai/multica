package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func defaultChatFixture(t *testing.T) (*testutil.Fixture, string, string) {
	t.Helper()
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Chat defaults", "slug": "chat-default-" + uuid.NewString()})
	fx := testutil.New(testPool, wsID, testUserID)
	fx.Insert(t, "member", testutil.Cols{"workspace_id": wsID, "user_id": testUserID, "role": "owner"})
	runtimeID := fx.Runtime(t, "Default runtime")
	return fx, wsID, fx.Agent(t, "Default agent", runtimeID)
}

func updateDefaultWorkspace(t *testing.T, wsID, userID string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParam(newRequest("PATCH", "/api/workspaces/"+wsID, body), "id", wsID)
	req.Header.Set("X-User-ID", userID)
	return testutil.Call(t, testHandler.UpdateWorkspace, req).Want(want).ResponseRecorder
}

func readDefaultSettings(t *testing.T, wsID string) map[string]any {
	t.Helper()
	var raw []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id=$1`, wsID).Scan(&raw)
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestWorkspaceDefaultChatAgent_InputAndPersistence(t *testing.T) {
	_, wsID, agentID := defaultChatFixture(t)
	dbfx.Exec(t, `UPDATE workspace SET settings='{"nested":{"keep":[1,true]},"unknown":7}' WHERE id=$1`, wsID)
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": agentID}, 200)
	settings := readDefaultSettings(t, wsID)
	if settings[defaultChatAgentKey] != agentID || settings["unknown"] != float64(7) || settings["nested"] == nil {
		t.Fatalf("default-only clobbered settings: %v", settings)
	}
	for _, legacy := range []map[string]any{{"other": true}, {"other": true, defaultChatAgentKey: "stale"}, {defaultChatAgentKey: nil}} {
		updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"settings": legacy}, 200)
		if got := readDefaultSettings(t, wsID)[defaultChatAgentKey]; got != agentID {
			t.Fatalf("legacy overwrote default: %v", got)
		}
	}
	for _, body := range []map[string]any{{"name": "Omitted"}, {"settings": nil}} {
		updateDefaultWorkspace(t, wsID, testUserID, body, 200)
		if got := readDefaultSettings(t, wsID)[defaultChatAgentKey]; got != agentID {
			t.Fatal(got)
		}
	}
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": nil}, 200)
	if _, ok := readDefaultSettings(t, wsID)[defaultChatAgentKey]; ok {
		t.Fatal("clear must remove key")
	}
	// A stale client cannot resurrect a cleared setting.
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"settings": map[string]any{defaultChatAgentKey: agentID, "other": true}}, 200)
	if _, ok := readDefaultSettings(t, wsID)[defaultChatAgentKey]; ok {
		t.Fatal("legacy resurrected default")
	}
	for _, value := range []any{"", "bad", 42, true, []any{}, map[string]any{}} {
		t.Run("invalid-"+string(defaultChatJSON(t, value)), func(t *testing.T) {
			before := defaultChatJSON(t, readDefaultSettings(t, wsID))
			updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": value, "name": "MUST NOT WRITE", "settings": map[string]any{"bad": true}}, 400)
			if !bytes.Equal(before, defaultChatJSON(t, readDefaultSettings(t, wsID))) {
				t.Fatal("invalid input partially wrote settings")
			}
			var name string
			dbfx.QueryRow(t, `SELECT name FROM workspace WHERE id=$1`, wsID).Scan(&name)
			if name == "MUST NOT WRITE" {
				t.Fatal("invalid input partially wrote name")
			}
		})
	}
	for _, value := range []any{42, true, []any{}, "bad"} {
		updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"settings": value, "default_chat_agent_id": agentID}, 400)
		if _, ok := readDefaultSettings(t, wsID)[defaultChatAgentKey]; ok {
			t.Fatal("invalid settings partially set default")
		}
	}
	// Historical scalar settings are normalized only when an object edit is made.
	dbfx.Exec(t, `UPDATE workspace SET settings='[1,true]' WHERE id=$1`, wsID)
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"name": "Historical", "settings": nil}, 200)
	var legacy []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id=$1`, wsID).Scan(&legacy)
	if string(legacy) != "[1, true]" {
		t.Fatalf("omitted settings changed historical shape: %s", legacy)
	}
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": agentID}, 200)
	if got := readDefaultSettings(t, wsID)[defaultChatAgentKey]; got != agentID {
		t.Fatal(got)
	}
}

func defaultChatJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestWorkspaceDefaultChatAgent_CandidateValidation(t *testing.T) {
	fx, wsID, agentID := defaultChatFixture(t)
	otherUser := fx.Insert(t, "user", testutil.Cols{"name": "Other owner", "email": uuid.NewString() + "@example.test"})
	foreignWS := fx.Insert(t, "workspace", testutil.Cols{"name": "Other", "slug": uuid.NewString()})
	foreign := fx.Agent(t, "Foreign", "", testutil.Cols{"workspace_id": foreignWS})
	archived := fx.Agent(t, "Archived", handlerTestRuntimeID(t), testutil.Cols{"archived_at": testutil.Raw("now()")})
	noRuntime := fx.Agent(t, "Unbound", "")
	private := fx.Agent(t, "Private", handlerTestRuntimeID(t), testutil.Cols{"owner_id": otherUser})
	restricted := fx.Agent(t, "Restricted", handlerTestRuntimeID(t), testutil.Cols{"owner_id": otherUser, "permission_mode": "public_to"})
	blockedRuntime := fx.Runtime(t, "Broken", testutil.Cols{"status": "offline", "metadata": testutil.Raw(`'{"offline_reason":{"code":"not_executable"}}'::jsonb`)})
	blocked := fx.Agent(t, "Broken", blockedRuntime)
	ownerMismatch := fx.Agent(t, "Wrong runtime owner", handlerTestRuntimeID(t), testutil.Cols{"owner_id": otherUser, "permission_mode": "public_to"})
	for _, a := range []string{ownerMismatch, restricted} {
		fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": a, "target_type": "member", "target_id": testUserID})
	}
	// Restricted is callable now; remove the grant to verify it is rejected.
	fx.Exec(t, `DELETE FROM agent_invocation_target WHERE agent_id=$1`, restricted)
	for _, id := range []string{uuid.NewString(), foreign, archived, noRuntime, private, restricted, blocked, ownerMismatch} {
		t.Run(id, func(t *testing.T) {
			w := updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": id, "name": "MUST NOT WRITE"}, 400)
			if bytes.Contains(w.Body.Bytes(), []byte(id)) {
				t.Fatal("rejected ID leaked in error")
			}
			if _, ok := readDefaultSettings(t, wsID)[defaultChatAgentKey]; ok {
				t.Fatal("invalid candidate persisted")
			}
		})
	}
	// Offline is waitable, and member allow-lists are honored independently of admin visibility.
	fx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": agentID}, 200)
	fx.Exec(t, `UPDATE agent_runtime SET visibility='public' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)
	fx.Exec(t, `UPDATE agent SET runtime_id=(SELECT runtime_id FROM agent WHERE id=$1) WHERE id=$2`, agentID, restricted)
	fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": restricted, "target_type": "member", "target_id": testUserID})
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": restricted}, 200)
}

func TestWorkspaceDefaultChatAgent_ResponsePrivacy(t *testing.T) {
	fx, wsID, agentID := defaultChatFixture(t)
	admin := fx.Insert(t, "user", testutil.Cols{"name": "Admin", "email": uuid.NewString() + "@example.test"})
	member := fx.Insert(t, "user", testutil.Cols{"name": "Member", "email": uuid.NewString() + "@example.test"})
	for user, role := range map[string]string{admin: "admin", member: "member"} {
		fx.Insert(t, "member", testutil.Cols{"workspace_id": wsID, "user_id": user, "role": role})
	}
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": agentID, "settings": map[string]any{"nested": map[string]any{"keep": true}}}, 200)
	for _, userID := range []string{testUserID, admin, member} {
		for _, endpoint := range []string{"list", "get", "update"} {
			t.Run(userID+"/"+endpoint, func(t *testing.T) {
				req := withURLParam(newRequest("GET", "/api/workspaces/"+wsID, nil), "id", wsID)
				req.Header.Set("X-User-ID", userID)
				var w *httptest.ResponseRecorder
				switch endpoint {
				case "list":
					w = testutil.Call(t, testHandler.ListWorkspaces, req).Want(200).ResponseRecorder
				case "get":
					w = testutil.Call(t, testHandler.GetWorkspace, req).Want(200).ResponseRecorder
				case "update":
					w = updateDefaultWorkspace(t, wsID, userID, map[string]any{"description": "safe"}, 200)
				}
				if bytes.Contains(w.Body.Bytes(), []byte(agentID)) != (userID == testUserID) {
					t.Fatalf("incorrect full HTTP projection: %s", w.Body.String())
				}
				if !bytes.Contains(w.Body.Bytes(), []byte("nested")) {
					t.Fatal("lost unrelated settings")
				}
			})
		}
	}
	// Capture the exact JSON passed to workspace broadcast without retaining a
	// subscriber on the shared handler's bus between tests.
	h := *testHandler
	h.Bus = events.New()
	var broadcast []byte
	h.Bus.Subscribe(protocol.EventWorkspaceUpdated, func(e events.Event) { broadcast = defaultChatJSON(t, e) })
	req := withURLParam(newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{"default_chat_agent_id": agentID}), "id", wsID)
	testutil.Call(t, h.UpdateWorkspace, req).Want(200)
	if len(broadcast) == 0 || bytes.Contains(broadcast, []byte(agentID)) || !bytes.Contains(broadcast, []byte("nested")) {
		t.Fatalf("unsafe broadcast: %s", broadcast)
	}
	// Both daemon routes use this serializer; also exercise the actual repos handler.
	var raw []byte
	fx.QueryRow(t, `SELECT settings FROM workspace WHERE id=$1`, wsID).Scan(&raw)
	daemon := defaultChatJSON(t, workspaceReposResponse(wsID, []byte(`[]`), raw, nil))
	if bytes.Contains(daemon, []byte(agentID)) || !bytes.Contains(daemon, []byte("nested")) {
		t.Fatalf("unsafe daemon serialization: %s", daemon)
	}
	req = withURLParam(newDaemonTokenRequest("GET", "/api/daemon/workspaces/"+wsID+"/repos", nil, wsID, "default-chat-daemon"), "workspaceId", wsID)
	req.Header.Set("X-Workspace-ID", wsID)
	req.Header.Set("X-Daemon-Workspace-ID", wsID)
	w := testutil.Call(t, testHandler.GetDaemonWorkspaceRepos, req).Want(200)
	if bytes.Contains(w.Body.Bytes(), []byte(agentID)) {
		t.Fatal("repos handler leaked ID")
	}
	req = newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": wsID, "daemon_id": "default-chat-daemon", "device_name": "Test",
		"runtimes": []map[string]any{{"name": "Test", "type": "claude", "version": "1.0.0", "status": "online"}},
	}, wsID, "default-chat-daemon")
	register := testutil.Call(t, testHandler.DaemonRegister, req).Want(200)
	if bytes.Contains(register.Body.Bytes(), []byte(agentID)) || !bytes.Contains(register.Body.Bytes(), []byte("nested")) {
		t.Fatal("daemon register leaked default or lost settings")
	}

	// A valid member-target default is returned to that member, then disappears
	// immediately on revocation, archive, deletion, malformed config or DB failure.
	fx.Exec(t, `UPDATE agent SET permission_mode='public_to' WHERE id=$1`, agentID)
	fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agentID, "target_type": "member", "target_id": member})
	ws, err := testHandler.Queries.GetWorkspace(context.Background(), parseUUID(wsID))
	if err != nil {
		t.Fatal(err)
	}
	response := func(ctx context.Context) []byte {
		r := newRequest("GET", "/", nil).WithContext(ctx)
		r.Header.Set("X-User-ID", member)
		return defaultChatJSON(t, testHandler.workspaceResponseForUser(r, ws))
	}
	if !bytes.Contains(response(context.Background()), []byte(agentID)) {
		t.Fatal("allow-list projection missing")
	}
	// Repeat both actual daemon HTTP responses with a member-restricted default.
	for _, call := range []struct {
		path    string
		handler http.HandlerFunc
		body    any
	}{
		{"/api/daemon/workspaces/" + wsID + "/repos", testHandler.GetDaemonWorkspaceRepos, nil},
		{"/api/daemon/register", testHandler.DaemonRegister, map[string]any{"workspace_id": wsID, "daemon_id": "default-chat-daemon", "device_name": "Test", "runtimes": []map[string]any{{"name": "Test", "type": "claude", "version": "1.0.0", "status": "online"}}}},
	} {
		method := "GET"
		if call.body != nil {
			method = "POST"
		}
		r := withURLParam(newDaemonTokenRequest(method, call.path, call.body, wsID, "default-chat-daemon"), "workspaceId", wsID)
		w := testutil.Call(t, call.handler, r).Want(200)
		if bytes.Contains(w.Body.Bytes(), []byte(agentID)) || !bytes.Contains(w.Body.Bytes(), []byte("nested")) {
			t.Fatalf("restricted daemon response leaked ID/lost settings: %s", w.Body.String())
		}
	}
	fx.Exec(t, `UPDATE agent_runtime SET status='offline', metadata='{"offline_reason":{"code":"not_executable"}}' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)
	if bytes.Contains(response(context.Background()), []byte(agentID)) {
		t.Fatal("blocked runtime default leaked")
	}
	fx.Exec(t, `UPDATE agent_runtime SET status='online', metadata='{}' WHERE id=(SELECT runtime_id FROM agent WHERE id=$1)`, agentID)

	fx.Exec(t, `DELETE FROM agent_invocation_target WHERE agent_id=$1`, agentID)
	if bytes.Contains(response(context.Background()), []byte(agentID)) {
		t.Fatal("revoked default leaked")
	}
	fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agentID, "target_type": "member", "target_id": member})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if bytes.Contains(response(ctx), []byte(agentID)) {
		t.Fatal("query error did not fail closed")
	}
	fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, agentID)
	if bytes.Contains(response(context.Background()), []byte(agentID)) {
		t.Fatal("archived default leaked")
	}
	fx.Exec(t, `DELETE FROM agent WHERE id=$1`, agentID)
	if bytes.Contains(response(context.Background()), []byte(agentID)) {
		t.Fatal("deleted default leaked")
	}
	for _, bad := range []string{`{"default_chat_agent_id":"bad","other":true}`, `{"default_chat_agent_id":["` + agentID + `"],"other":true}`} {
		ws.Settings = []byte(bad)
		if bytes.Contains(response(context.Background()), []byte(agentID)) {
			t.Fatal("malformed config leaked")
		}
	}
}

func TestWorkspaceDefaultChatAgent_ConcurrentLegacyWrites(t *testing.T) {
	_, wsID, agentID := defaultChatFixture(t)
	for _, clear := range []bool{false, true} {
		for _, defaultFirst := range []bool{true, false} {
			t.Run(map[bool]string{true: "clear/", false: "set/"}[clear]+map[bool]string{true: "default-before-legacy", false: "legacy-before-default"}[defaultFirst], func(t *testing.T) {
				dbfx.Exec(t, `UPDATE workspace SET settings=jsonb_build_object('initial',true,'default_chat_agent_id',$2::text) WHERE id=$1`, wsID, agentID)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				tx, err := testPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				defaultParams := db.UpdateWorkspaceParams{ID: parseUUID(wsID), DefaultChatAgentPresent: true, DefaultChatAgentID: pgtype.Text{String: agentID, Valid: true}}
				if clear {
					defaultParams.DefaultChatAgentID = pgtype.Text{}
				}
				legacyParams := db.UpdateWorkspaceParams{ID: parseUUID(wsID), Settings: []byte(`{"legacy":{"keep":true},"default_chat_agent_id":"stale"}`)}
				first, second := defaultParams, legacyParams
				if !defaultFirst {
					first, second = second, first
				}
				if _, err = db.New(tx).UpdateWorkspace(ctx, first); err != nil {
					t.Fatal(err)
				}
				conn, err := testPool.Acquire(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Release()
				result := make(chan error, 1)
				go func() { _, err := db.New(conn).UpdateWorkspace(ctx, second); result <- err }()
				// Observe PostgreSQL's lock state, rather than assuming a goroutine ran.
				blocked := false
				for !blocked {
					if err := testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, conn.Conn().PgConn().PID()).Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					if !blocked {
						select {
						case err := <-result:
							t.Fatalf("writer did not wait for lock: %v", err)
						case <-ctx.Done():
							t.Fatal(ctx.Err())
						case <-time.After(5 * time.Millisecond):
						}
					}
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				settings := readDefaultSettings(t, wsID)
				if (clear && settings[defaultChatAgentKey] != nil) || (!clear && settings[defaultChatAgentKey] != agentID) || settings["legacy"] == nil {
					t.Fatalf("concurrent update clobbered data: %v", settings)
				}
			})
		}
	}
}

func TestWithoutDefaultChatAgent_PreservesOtherShapes(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `[1,{"nested":true}]`, `"legacy"`, `42`, `{"other":{"nested":[1,true]}}`} {
		var before, after any
		if err := json.Unmarshal([]byte(raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(withoutDefaultChatAgent([]byte(raw)), &after); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(defaultChatJSON(t, before), defaultChatJSON(t, after)) {
			t.Fatalf("changed shape: %s", raw)
		}
	}
}

// Exercise a runtime lookup error after successful scoped agent/permission
// reads. This must not leak the configured ID or admit a write.
type defaultChatRuntimeFailureDB struct{ db.DBTX }

func (q defaultChatRuntimeFailureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "FROM agent_runtime") {
		return defaultChatErrorRow{}
	}
	return q.DBTX.QueryRow(ctx, sql, args...)
}

type defaultChatErrorRow struct{}

func (defaultChatErrorRow) Scan(...any) error { return errors.New("runtime lookup unavailable") }

func TestWorkspaceDefaultChatAgent_RuntimeLookupError(t *testing.T) {
	_, wsID, agentID := defaultChatFixture(t)
	updateDefaultWorkspace(t, wsID, testUserID, map[string]any{"default_chat_agent_id": agentID}, 200)
	ws, err := testHandler.Queries.GetWorkspace(context.Background(), parseUUID(wsID))
	if err != nil {
		t.Fatal(err)
	}
	h := *testHandler
	h.Queries = db.New(defaultChatRuntimeFailureDB{DBTX: testPool})
	req := newRequest("GET", "/", nil)
	if bytes.Contains(defaultChatJSON(t, h.workspaceResponseForUser(req, ws)), []byte(agentID)) {
		t.Fatal("runtime query failure leaked ID")
	}
	req = withURLParam(newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{"default_chat_agent_id": agentID, "name": "MUST NOT WRITE"}), "id", wsID)
	w := testutil.Call(t, h.UpdateWorkspace, req).Want(400)
	if bytes.Contains(w.Body.Bytes(), []byte(agentID)) {
		t.Fatal("runtime query failure leaked error")
	}
	var name string
	dbfx.QueryRow(t, `SELECT name FROM workspace WHERE id=$1`, wsID).Scan(&name)
	if name == "MUST NOT WRITE" {
		t.Fatal("runtime lookup failure partially wrote")
	}
}

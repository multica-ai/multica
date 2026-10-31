package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func recoveryRequest(t *testing.T, user, method, path string, body any, params ...string) *http.Request {
	t.Helper()
	req := testutil.WithURLParams(newRequestAs(user, method, path, body), params...)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(user), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}
func recoveryFixture(t *testing.T) (string, string, CreateAgentBuilderSessionResponse) {
	t.Helper()
	if testHandler == nil {
		t.Fatal("isolated database required")
	}
	p := insertRuntimeProfileFixture(t, context.Background(), "Recovery", "codex", "recovery-test")
	r := insertProfileRuntimeFixture(t, context.Background(), p, "Recovery host", "codex")
	var s CreateAgentBuilderSessionResponse
	testutil.Call(t, testHandler.CreateAgentBuilderSession, recoveryRequest(t, testUserID, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": r})).Want(201).JSON(&s)
	return p, r, s
}
func recoveryPage(t *testing.T, user, p, extra string) ListEmptyAgentBuilderSessionsResponse {
	t.Helper()
	var out ListEmptyAgentBuilderSessionsResponse
	testutil.Call(t, testHandler.ListEmptyAgentBuilderSessions, recoveryRequest(t, user, "GET", "/api/agent-builder/sessions/empty?profile_id="+p+extra, nil)).Want(200).JSON(&out)
	return out
}
func recoveryDiscardRequest(t *testing.T, user, p string, row EmptyAgentBuilderSession) *http.Request {
	t.Helper()
	return recoveryRequest(t, user, "POST", "/api/agent-builder/sessions/"+row.SessionID+"/discard-empty", DiscardEmptyAgentBuilderSessionRequest{ProfileID: p, Snapshot: row.Snapshot}, "sessionId", row.SessionID)
}
func recoveryDiscard(t *testing.T, p string, row EmptyAgentBuilderSession, want int) {
	t.Helper()
	testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, recoveryDiscardRequest(t, testUserID, p, row)).Want(want)
}

type lostRecoveryResponse struct {
	header http.Header
	code   int
}

func (w *lostRecoveryResponse) Header() http.Header       { return w.header }
func (w *lostRecoveryResponse) WriteHeader(n int)         { w.code = n }
func (w *lostRecoveryResponse) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestEmptyBuilderRecoveryLostResponse(t *testing.T) {
	p, r, initial := recoveryFixture(t)
	// Remove the initial fixture through the same conditional production path.
	recoveryDiscard(t, p, recoveryPage(t, testUserID, p, "").Sessions[0], 204)
	_ = initial
	w := &lostRecoveryResponse{header: make(http.Header)}
	testHandler.CreateAgentBuilderSession(w, recoveryRequest(t, testUserID, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": r}))
	if w.code != 201 {
		t.Fatalf("lost create status %d", w.code)
	}
	page := recoveryPage(t, testUserID, p, "")
	if len(page.Sessions) != 1 || page.NextCursor != "" {
		t.Fatalf("discovery: %+v", page)
	}
	row := page.Sessions[0]
	var ordinary ListAgentBuilderSessionsResponse
	testutil.Call(t, testHandler.ListAgentBuilderSessions, recoveryRequest(t, testUserID, "GET", "/api/agent-builder/sessions", nil)).Want(200).JSON(&ordinary)
	for _, s := range ordinary.Sessions {
		if s.SessionID == row.SessionID {
			t.Fatal("empty session entered ordinary drafts")
		}
	}
	// The profile guard remains active until explicit conditional discard.
	req := recoveryRequest(t, testUserID, "DELETE", "/", nil, "id", testWorkspaceID, "profileId", p)
	testutil.Call(t, testHandler.DeleteRuntimeProfile, req).Want(409)
	var eventCount atomic.Int32
	var uncommittedEvent atomic.Bool
	testHandler.Bus.Subscribe(protocol.EventChatSessionDeleted, func(e events.Event) {
		if e.ChatSessionID != row.SessionID {
			return
		}
		if dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, row.SessionID) != 0 {
			uncommittedEvent.Store(true)
		}
		eventCount.Add(1)
	})
	recoveryDiscard(t, p, row, 204)
	if eventCount.Load() != 1 || uncommittedEvent.Load() {
		t.Fatal("delete event must follow commit exactly once")
	}
	recoveryDiscard(t, p, row, 404)
	if eventCount.Load() != 1 {
		t.Fatal("missing-session retry repeated event")
	}
	testutil.Call(t, testHandler.DeleteRuntimeProfile, recoveryRequest(t, testUserID, "DELETE", "/", nil, "id", testWorkspaceID, "profileId", p)).Want(204)
}

func TestEmptyBuilderRecoveryEligibility(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"message", `INSERT INTO chat_message(chat_session_id,role,content) VALUES($1,'user','work')`},
		{"saved-draft", `INSERT INTO agent_builder_draft(chat_session_id,workspace_id,draft) SELECT id,workspace_id,'{}' FROM chat_session WHERE id=$1`},
		{"archived-session", `UPDATE chat_session SET status='archived' WHERE id=$1`},
		{"archived-carrier", `UPDATE agent SET archived_at=now() WHERE id=(SELECT agent_id FROM chat_session WHERE id=$1)`},
		{"wrong-kind", `UPDATE agent SET kind='user' WHERE id=(SELECT agent_id FROM chat_session WHERE id=$1)`},
		{"near-prefix-dash", `UPDATE agent SET system_key='agent-builder:x' WHERE id=(SELECT agent_id FROM chat_session WHERE id=$1)`},
		{"near-prefix-wildcard", `UPDATE agent SET system_key='agentXbuilder:x' WHERE id=(SELECT agent_id FROM chat_session WHERE id=$1)`},
		{"leading-space", `UPDATE agent SET system_key=' agent_builder:x' WHERE id=(SELECT agent_id FROM chat_session WHERE id=$1)`},
		{"other-session", `INSERT INTO chat_session(workspace_id,creator_id,agent_id,title) SELECT workspace_id,creator_id,agent_id,'second' FROM chat_session WHERE id=$1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _, s := recoveryFixture(t)
			row := recoveryPage(t, testUserID, p, "").Sessions[0]
			dbfx.Exec(t, tc.sql, s.SessionID)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_builder_draft WHERE chat_session_id=$1`, s.SessionID)
			})
			if len(recoveryPage(t, testUserID, p, "").Sessions) != 0 {
				t.Fatal("ineligible session listed")
			}
			recoveryDiscard(t, p, row, 409)
			if dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, s.SessionID) != 1 {
				t.Fatal("ineligible session removed")
			}
		})
	}
	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred", "completed", "failed", "cancelled"} {
		t.Run("task-"+status, func(t *testing.T) {
			p, _, s := recoveryFixture(t)
			row := recoveryPage(t, testUserID, p, "").Sessions[0]
			id := dbfx.Task(t, s.BuilderAgentID, testutil.Cols{"chat_session_id": s.SessionID, "status": status, "runtime_id": testRuntimeID})
			if len(recoveryPage(t, testUserID, p, "").Sessions) != 0 {
				t.Fatal("task history listed")
			}
			recoveryDiscard(t, p, row, 409)
			var actual string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, id).Scan(&actual)
			if actual != status {
				t.Fatalf("task changed %s -> %s", status, actual)
			}
		})
	}
	t.Run("carrier-task-without-session", func(t *testing.T) {
		p, _, s := recoveryFixture(t)
		row := recoveryPage(t, testUserID, p, "").Sessions[0]
		dbfx.Task(t, s.BuilderAgentID, testutil.Cols{"runtime_id": testRuntimeID})
		if len(recoveryPage(t, testUserID, p, "").Sessions) != 0 {
			t.Fatal("carrier work listed")
		}
		recoveryDiscard(t, p, row, 409)
	})
	t.Run("draft-restore", func(t *testing.T) {
		p, _, s := recoveryFixture(t)
		row := recoveryPage(t, testUserID, p, "").Sessions[0]
		dbfx.Insert(t, "chat_draft_restore", testutil.Cols{"chat_session_id": s.SessionID, "id": testutil.Raw("gen_random_uuid()"), "content": "restore", "attachment_ids": testutil.Raw("'{}'::uuid[]"), "task_id": testutil.Raw("gen_random_uuid()")})
		if len(recoveryPage(t, testUserID, p, "").Sessions) != 0 {
			t.Fatal("deferred input listed")
		}
		recoveryDiscard(t, p, row, 409)
	})
	t.Run("offline", func(t *testing.T) {
		p, r, _ := recoveryFixture(t)
		row := recoveryPage(t, testUserID, p, "").Sessions[0]
		dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, r)
		if len(recoveryPage(t, testUserID, p, "").Sessions) != 1 {
			t.Fatal("offline recovery unavailable")
		}
		recoveryDiscard(t, p, row, 204)
	})
}

func TestEmptyBuilderRecoveryScopeAndPagination(t *testing.T) {
	p, r, s := recoveryFixture(t)
	row := recoveryPage(t, testUserID, p, "").Sessions[0]
	other := dbfx.User(t, "Other creator", "recovery-other@example.test")
	dbfx.Member(t, testWorkspaceID, other, "admin")
	if len(recoveryPage(t, other, p, "").Sessions) != 0 {
		t.Fatal("admin read another creator")
	}
	testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, recoveryDiscardRequest(t, other, p, row)).Want(403)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility='public' WHERE id=$1`, r)
	memberCreator := dbfx.User(t, "Member creator", "recovery-member@example.test")
	dbfx.Member(t, testWorkspaceID, memberCreator, "member")
	var memberSession CreateAgentBuilderSessionResponse
	testutil.Call(t, testHandler.CreateAgentBuilderSession, recoveryRequest(t, memberCreator, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": r})).Want(201).JSON(&memberSession)
	memberPage := recoveryPage(t, memberCreator, p, "")
	if len(memberPage.Sessions) != 1 || memberPage.Sessions[0].SessionID != memberSession.SessionID {
		t.Fatal("ordinary creator cannot recover own session")
	}
	if len(recoveryPage(t, testUserID, p, "").Sessions) != 1 {
		t.Fatal("workspace owner saw member's session")
	}
	testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, recoveryDiscardRequest(t, memberCreator, p, memberPage.Sessions[0])).Want(204)
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := recoveryRequest(t, testUserID, "GET", "/api/agent-builder/sessions/empty?profile_id="+p, nil)
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.ListEmptyAgentBuilderSessions, req).Want(403)
		req = recoveryDiscardRequest(t, testUserID, p, row)
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, req).Want(403)
	}
	otherWS := dbfx.Workspace(t, "Other recovery workspace", "other-recovery-workspace")
	dbfx.Member(t, otherWS, testUserID, "owner")
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(testUserID), WorkspaceID: parseUUID(otherWS)})
	if err != nil {
		t.Fatal(err)
	}
	req := recoveryDiscardRequest(t, testUserID, p, row)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), otherWS, member))
	testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, req).Want(404)
	// Enough rows to exercise stable UUID keyset pages, without returning content.
	for i := 0; i < 4; i++ {
		testutil.Call(t, testHandler.CreateAgentBuilderSession, recoveryRequest(t, testUserID, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": r})).Want(201)
	}
	seen := map[string]bool{}
	cursor := ""
	last := ""
	pages := 0
	for {
		page := recoveryPage(t, testUserID, p, "&page_size=2"+cursor)
		pages++
		for _, v := range page.Sessions {
			if seen[v.SessionID] || v.SessionID <= last {
				t.Fatal("unstable or repeated page")
			}
			seen[v.SessionID] = true
			last = v.SessionID
		}
		if page.NextCursor == "" {
			break
		}
		cursor = "&cursor=" + url.QueryEscape(page.NextCursor)
		if pages > 3 {
			t.Fatal("pagination loop")
		}
	}
	if len(seen) != 5 || !seen[s.SessionID] || pages != 3 {
		t.Fatal(seen, pages)
	}
	first := recoveryPage(t, testUserID, p, "&page_size=2")
	again := recoveryPage(t, testUserID, p, "&page_size=2")
	if first.NextCursor != again.NextCursor {
		t.Fatal("cursor unstable")
	}
	scoped := base64.RawURLEncoding.EncodeToString([]byte(otherWS + ":" + testUserID + ":" + p + ":" + row.SessionID))
	for _, suffix := range []string{"&page_size=0", "&page_size=51", "&page_size=bad", "&page_size=1&page_size=2", "&cursor=", "&cursor=bad", "&cursor=" + scoped, "&cursor=x&cursor=y", "&profile_id=" + p} {
		testutil.Call(t, testHandler.ListEmptyAgentBuilderSessions, recoveryRequest(t, testUserID, "GET", "/api/agent-builder/sessions/empty?profile_id="+p+suffix, nil)).Want(400)
	}
	// A different profile must never authorize the snapshot's deletion.
	otherP := insertRuntimeProfileFixture(t, context.Background(), "Other", "codex", "recovery-other")
	recoveryDiscard(t, otherP, row, 409)
	if len(recoveryPage(t, testUserID, otherP, "").Sessions) != 0 {
		t.Fatal("profile filter ignored")
	}
}

func TestEmptyBuilderRecoveryRollback(t *testing.T) {
	p, _, s := recoveryFixture(t)
	row := recoveryPage(t, testUserID, p, "").Sessions[0]
	var eventsSeen atomic.Int32
	testHandler.Bus.Subscribe(protocol.EventChatSessionDeleted, func(e events.Event) {
		if e.ChatSessionID == s.SessionID {
			eventsSeen.Add(1)
		}
	})
	dbfx.Exec(t, `CREATE FUNCTION recovery_reject_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.id::text='`+s.BuilderAgentID+`' THEN RAISE EXCEPTION 'injected recovery failure'; END IF; RETURN OLD; END $$`)
	dbfx.Exec(t, `CREATE TRIGGER recovery_reject_delete BEFORE DELETE ON agent FOR EACH ROW EXECUTE FUNCTION recovery_reject_delete()`)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS recovery_reject_delete ON agent`)
		testPool.Exec(context.Background(), `DROP FUNCTION IF EXISTS recovery_reject_delete()`)
	})
	recoveryDiscard(t, p, row, 500)
	if eventsSeen.Load() != 0 || dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, s.SessionID) != 1 || dbfx.Count(t, `SELECT count(*) FROM agent WHERE id=$1`, s.BuilderAgentID) != 1 {
		t.Fatal("rollback lost data or emitted event")
	}
	dbfx.Exec(t, `DROP TRIGGER recovery_reject_delete ON agent`)
	dbfx.Exec(t, `DROP FUNCTION recovery_reject_delete()`)
	recoveryDiscard(t, p, row, 204)
	if eventsSeen.Load() != 1 {
		t.Fatal("retry did not publish committed delete")
	}
}

// Pause real production transactions immediately before commit. This makes
// both serializations deterministic without sleeps deciding who won a race.
type recoveryCommitGate struct {
	pool    *pgxpool.Pool
	reached chan struct{}
	release chan struct{}
	pid     chan int
}
type recoveryGatedTx struct {
	pgx.Tx
	gate *recoveryCommitGate
}

func (g *recoveryCommitGate) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	g.pid <- int(tx.Conn().PgConn().PID())
	return &recoveryGatedTx{Tx: tx, gate: g}, nil
}
func (tx *recoveryGatedTx) Commit(ctx context.Context) error {
	close(tx.gate.reached)
	select {
	case <-tx.gate.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return tx.Tx.Commit(ctx)
}
func newRecoveryGate() *recoveryCommitGate {
	return &recoveryCommitGate{pool: testPool, reached: make(chan struct{}), release: make(chan struct{}), pid: make(chan int, 1)}
}
func recoveryWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("transaction did not reach gate")
	}
}

func TestEmptyBuilderRecoveryConcurrentWriters(t *testing.T) {
	for _, kind := range []string{"save", "send", "archive", "rebind"} {
		for _, first := range []string{"writer", "discard"} {
			t.Run(kind+"-"+first+"-first", func(t *testing.T) {
				p, _, s := recoveryFixture(t)
				row := recoveryPage(t, testUserID, p, "").Sessions[0]
				gate := newRecoveryGate()
				var released atomic.Bool
				t.Cleanup(func() {
					if released.CompareAndSwap(false, true) {
						close(gate.release)
					}
				})
				gated := *testHandler
				gated.TxStarter = gate
				gated.TaskService = service.NewTaskService(gated.Queries, gate, gated.Hub, gated.Bus)
				writer := func(h *Handler, w http.ResponseWriter) {
					switch kind {
					case "save":
						h.SaveAgentBuilderDraft(w, recoveryRequest(t, testUserID, "PUT", "/", map[string]any{"draft": map[string]any{"name": "Saved concurrently"}}, "sessionId", s.SessionID))
					case "send":
						h.SendChatMessage(w, recoveryRequest(t, testUserID, "POST", "/", map[string]any{"content": "Keep this work"}, "sessionId", s.SessionID))
					case "archive":
						h.SetChatSessionArchived(w, recoveryRequest(t, testUserID, "PATCH", "/", map[string]any{"archived": true}, "sessionId", s.SessionID))
					case "rebind":
						h.SwitchAgentBuilderRuntime(w, recoveryRequest(t, testUserID, "PATCH", "/", map[string]any{"runtime_id": testRuntimeID}, "sessionId", s.SessionID))
					}
				}
				a, b := httptest.NewRecorder(), httptest.NewRecorder()
				doneA, doneB := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(doneA)
					if first == "writer" {
						writer(&gated, a)
					} else {
						gated.DiscardEmptyAgentBuilderSession(a, recoveryDiscardRequest(t, testUserID, p, row))
					}
				}()
				recoveryWait(t, gate.reached)
				pid := <-gate.pid
				go func() {
					defer close(doneB)
					if first == "writer" {
						testHandler.DiscardEmptyAgentBuilderSession(b, recoveryDiscardRequest(t, testUserID, p, row))
					} else {
						writer(testHandler, b)
					}
				}()
				if !waitForWaiterBlockedBy(t, pid, 10*time.Second) {
					t.Fatal("second operation did not wait for first transaction")
				}
				released.Store(true)
				close(gate.release)
				recoveryWait(t, doneA)
				recoveryWait(t, doneB)
				if first == "writer" {
					want := map[string]int{"save": 204, "send": 201, "archive": 200, "rebind": 200}[kind]
					if a.Code != want || b.Code != 409 {
						t.Fatalf("writer=%d %s discard=%d %s", a.Code, a.Body, b.Code, b.Body)
					}
					if dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, s.SessionID) != 1 {
						t.Fatal("writer's session deleted")
					}
				} else {
					if a.Code != 204 || b.Code < 400 {
						t.Fatalf("discard=%d writer=%d %s", a.Code, b.Code, b.Body)
					}
					if dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, s.SessionID) != 0 || countBuilderDrafts(t, s.SessionID) != 0 {
						t.Fatal("writer recreated deleted data")
					}
				}
				t.Cleanup(func() {
					testPool.Exec(context.Background(), `DELETE FROM agent_builder_draft WHERE chat_session_id=$1`, s.SessionID)
				})
			})
		}
	}
}

func TestEmptyBuilderRecoveryBadRequest(t *testing.T) {
	p, _, s := recoveryFixture(t)
	for _, body := range []any{map[string]any{}, map[string]any{"profile_id": "bad", "snapshot": strings.Repeat("a", 64)}, map[string]any{"profile_id": p, "snapshot": ""}, "{"} {
		testutil.Call(t, testHandler.DiscardEmptyAgentBuilderSession, recoveryRequest(t, testUserID, "POST", "/", body, "sessionId", s.SessionID)).Want(400)
	}
	req := recoveryRequest(t, testUserID, "GET", "/api/agent-builder/sessions/empty?profile_id="+p, nil)
	req.Header.Del("X-User-ID")
	testutil.Call(t, testHandler.ListEmptyAgentBuilderSessions, req).Want(401)
	// Response is deliberately minimal, including a version instead of contents.
	raw, _ := json.Marshal(recoveryPage(t, testUserID, p, ""))
	for _, secret := range []string{"creator_id", "draft", "content", "instructions"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("metadata leaked", secret)
		}
	}
}

func TestEmptyBuilderRecoveryAgentLockRecheck(t *testing.T) {
	for _, change := range []string{"carrier-task", "profile-binding", "carrier-archive", "same-profile-rebind"} {
		t.Run(change, func(t *testing.T) {
			p, r, s := recoveryFixture(t)
			row := recoveryPage(t, testUserID, p, "").Sessions[0]
			tx, err := testPool.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(context.Background(), `SELECT id FROM agent WHERE id=$1 FOR UPDATE`, s.BuilderAgentID); err != nil {
				t.Fatal(err)
			}
			pid := int(tx.Conn().PgConn().PID())
			done := make(chan struct{})
			w := httptest.NewRecorder()
			go func() {
				defer close(done)
				testHandler.DiscardEmptyAgentBuilderSession(w, recoveryDiscardRequest(t, testUserID, p, row))
			}()
			if !waitForWaiterBlockedBy(t, pid, 10*time.Second) {
				t.Fatal("discard did not take agent lock after session lock")
			}
			switch change {
			case "carrier-task":
				if _, err = tx.Exec(context.Background(), `INSERT INTO agent_task_queue(agent_id,runtime_id,status,priority)VALUES($1,$2,'queued',0)`, s.BuilderAgentID, r); err != nil {
					t.Fatal(err)
				}
			case "profile-binding":
				otherP := insertRuntimeProfileFixture(t, context.Background(), "Changed profile", "codex", "changed-profile")
				if _, err = tx.Exec(context.Background(), `UPDATE agent_runtime SET profile_id=$2 WHERE id=$1`, r, otherP); err != nil {
					t.Fatal(err)
				}
			case "carrier-archive":
				if _, err = tx.Exec(context.Background(), `UPDATE agent SET archived_at=now() WHERE id=$1`, s.BuilderAgentID); err != nil {
					t.Fatal(err)
				}
			case "same-profile-rebind":
				target := insertProfileRuntimeFixture(t, context.Background(), p, "Other host", "codex")
				if _, err = tx.Exec(context.Background(), `UPDATE agent SET runtime_id=$2,updated_at=now() WHERE id=$1`, s.BuilderAgentID, target); err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(context.Background()); err != nil {
				t.Fatal(err)
			}
			recoveryWait(t, done)
			if w.Code != 409 {
				t.Fatalf("changed carrier: %d %s", w.Code, w.Body)
			}
			if dbfx.Count(t, `SELECT count(*) FROM chat_session WHERE id=$1`, s.SessionID) != 1 {
				t.Fatal("session lost")
			}
		})
	}
}

func TestEmptyBuilderRecoveryConcurrentProfileDelete(t *testing.T) {
	p, _, s := recoveryFixture(t)
	row := recoveryPage(t, testUserID, p, "").Sessions[0]
	gate := newRecoveryGate()
	gated := *testHandler
	gated.TxStarter = gate
	var released atomic.Bool
	t.Cleanup(func() {
		if released.CompareAndSwap(false, true) {
			close(gate.release)
		}
	})
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() {
		defer close(done)
		gated.DiscardEmptyAgentBuilderSession(w, recoveryDiscardRequest(t, testUserID, p, row))
	}()
	recoveryWait(t, gate.reached)
	// Until recovery commits the profile guard must continue to see the carrier.
	testutil.Call(t, testHandler.DeleteRuntimeProfile, recoveryRequest(t, testUserID, "DELETE", "/", nil, "id", testWorkspaceID, "profileId", p)).Want(409)
	released.Store(true)
	close(gate.release)
	recoveryWait(t, done)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if dbfx.Count(t, `SELECT count(*) FROM agent WHERE id=$1`, s.BuilderAgentID) != 0 {
		t.Fatal("carrier survived")
	}
	testutil.Call(t, testHandler.DeleteRuntimeProfile, recoveryRequest(t, testUserID, "DELETE", "/", nil, "id", testWorkspaceID, "profileId", p)).Want(204)
}

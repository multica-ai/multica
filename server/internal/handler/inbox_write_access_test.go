package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/inboxread"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Every inbox endpoint beyond the list reads, writes included, shares the
// list's owner gate: an agent acts on the runtime owner's inbox only when that
// owner triggered the task. This table drives the credential matrix and the
// allowed path; TestEveryInboxRouteDeniesMachineCredentialsWithoutOwnerTrigger
// (cmd/server) walks the router so every /api/inbox route, new ones included,
// denies the other human's run and cloud-node credentials.
type inboxOwnerEndpoint struct {
	name, method string
	// path receives the inbox item and its issue; param names the chi URL
	// parameter the handler reads, if any.
	path    func(item, issue string) string
	param   string
	body    map[string]any
	handler func(*Handler, http.ResponseWriter, *http.Request)
	// changes reports whether a successful call leaves the item read or archived.
	changes bool
}

var inboxOwnerEndpoints = []inboxOwnerEndpoint{
	{name: "read", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/read" }, param: "id", handler: (*Handler).MarkInboxRead, changes: true},
	{name: "unread", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/unread" }, param: "id", handler: (*Handler).MarkInboxUnread},
	{name: "archive", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/archive" }, param: "id", handler: (*Handler).ArchiveInboxItem, changes: true},
	{name: "unarchive", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/unarchive" }, param: "id", handler: (*Handler).UnarchiveInboxItem},
	{name: "snooze", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/snooze" }, param: "id",
		body: map[string]any{"until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, handler: (*Handler).SnoozeInboxItem},
	{name: "unsnooze", method: http.MethodPost, path: func(item, _ string) string { return "/api/inbox/" + item + "/unsnooze" }, param: "id", handler: (*Handler).UnsnoozeInboxItem},
	{name: "read issue", method: http.MethodPost, path: func(_, issue string) string { return "/api/inbox/issues/" + issue + "/read" }, param: "issueId", handler: (*Handler).MarkIssueInboxRead, changes: true},
	{name: "mark all read", method: http.MethodPost, path: func(_, _ string) string { return "/api/inbox/mark-all-read" }, handler: (*Handler).MarkAllInboxRead, changes: true},
	{name: "archive all", method: http.MethodPost, path: func(_, _ string) string { return "/api/inbox/archive-all" }, handler: (*Handler).ArchiveAllInbox, changes: true},
	{name: "archive all read", method: http.MethodPost, path: func(_, _ string) string { return "/api/inbox/archive-all-read" }, handler: (*Handler).ArchiveAllReadInbox},
	{name: "archive completed", method: http.MethodPost, path: func(_, _ string) string { return "/api/inbox/archive-completed" }, handler: (*Handler).ArchiveCompletedInbox},
	{name: "unread count", method: http.MethodGet, path: func(_, _ string) string { return "/api/inbox/unread-count" }, handler: (*Handler).CountUnreadInbox},
	{name: "unread summary", method: http.MethodGet, path: func(_, _ string) string { return "/api/inbox/unread-summary" }, handler: (*Handler).UnreadInboxSummary},
	{name: "snoozed", method: http.MethodGet, path: func(_, _ string) string { return "/api/inbox/snoozed" }, handler: (*Handler).ListSnoozedInbox},
}

type inboxWriteFixture struct {
	workspaceID, issueID, runtimeID, agentID string
}

func newInboxWriteFixture(t *testing.T) inboxWriteFixture {
	t.Helper()
	workspaceID := dbfx.Workspace(t, "Inbox writes", "inbox-writes-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	runtimeID := dbfx.Runtime(t, "Inbox write runtime", testutil.Cols{"workspace_id": workspaceID})
	return inboxWriteFixture{
		workspaceID: workspaceID,
		issueID:     dbfx.Issue(t, "Inbox write issue", testutil.Cols{"workspace_id": workspaceID}),
		runtimeID:   runtimeID,
		agentID:     dbfx.Agent(t, "Inbox write agent", runtimeID, testutil.Cols{"workspace_id": workspaceID}),
	}
}

func (f inboxWriteFixture) item(t *testing.T) string {
	t.Helper()
	return dbfx.Insert(t, "inbox_item", testutil.Cols{"workspace_id": f.workspaceID, "issue_id": f.issueID,
		"recipient_type": "member", "recipient_id": testUserID, "type": "new_comment", "severity": "info", "title": "Owner notice"})
}

func (f inboxWriteFixture) taskToken(t *testing.T, originator any) string {
	t.Helper()
	accountable := originator
	if accountable == nil {
		accountable = testUserID
	}
	taskID := dbfx.Task(t, f.agentID, testutil.Cols{"runtime_id": f.runtimeID, "status": "running",
		"originator_user_id": originator, "accountable_user_id": accountable})
	return inboxTaskCredential(t, taskID, f.agentID, f.workspaceID, testUserID)
}

func serveInboxEndpoint(handler func(*Handler, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return middleware.Auth(testHandler.Queries, nil, nil, nil)(
		middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler(testHandler, w, r)
		})),
	).ServeHTTP
}

func (e inboxOwnerEndpoint) request(f inboxWriteFixture, item, token string) *http.Request {
	var body any
	if e.body != nil {
		body = e.body
	}
	req := testutil.WithHeaders(testutil.JSONRequest(e.method, e.path(item, f.issueID), body),
		"Authorization", "Bearer "+token, "X-Workspace-ID", f.workspaceID)
	switch e.param {
	case "id":
		req = withURLParam(req, "id", item)
	case "issueId":
		req = withURLParam(req, "issueId", f.issueID)
	}
	return req
}

func requireInboxItemUntouched(t *testing.T, item string) {
	t.Helper()
	if state := loadInboxItemState(t, item); state != (inboxItemState{}) {
		t.Fatalf("denied call changed the owner's notification: %+v", state)
	}
	var snoozed bool
	dbfx.QueryRow(t, `SELECT snoozed_until IS NOT NULL FROM inbox_item WHERE id = $1`, item).Scan(&snoozed)
	if snoozed {
		t.Fatal("denied call snoozed the owner's notification")
	}
}

func TestInboxEndpointsShareOwnerGate(t *testing.T) {
	f := newInboxWriteFixture(t)
	other := createPlainMember(t, "inbox-write-other-"+uuid.NewString()+"@example.test")
	allowed := f.taskToken(t, testUserID)
	foreignTriggered := f.taskToken(t, other)
	unattributed := f.taskToken(t, nil)
	for _, e := range inboxOwnerEndpoints {
		t.Run(e.name, func(t *testing.T) {
			for name, token := range map[string]string{"another human": foreignTriggered, "unattributed": unattributed} {
				t.Run(name, func(t *testing.T) {
					item := f.item(t)
					testutil.Call(t, serveInboxEndpoint(e.handler), e.request(f, item, token)).Want(http.StatusForbidden)
					requireInboxItemUntouched(t, item)
				})
			}

			item := f.item(t)
			testutil.Call(t, serveInboxEndpoint(e.handler), e.request(f, item, allowed)).Want(http.StatusOK)
			if changed := loadInboxItemState(t, item) != (inboxItemState{}); changed != e.changes {
				t.Fatalf("self-triggered call changed=%v, want %v", changed, e.changes)
			}
		})
	}
}

// Per-item writes report how many notifications they actually changed, the
// issue group included, so repeating one reports 0.
func TestInboxItemWritesReportChangedCount(t *testing.T) {
	f := newInboxWriteFixture(t)
	token := f.taskToken(t, testUserID)
	endpoint := func(name string) inboxOwnerEndpoint {
		for _, e := range inboxOwnerEndpoints {
			if e.name == name {
				return e
			}
		}
		t.Fatalf("no endpoint %q", name)
		return inboxOwnerEndpoint{}
	}
	items := []string{f.item(t), f.item(t), f.item(t)}
	dbfx.Exec(t, `UPDATE inbox_item SET read = true WHERE id = $1`, items[2])
	for _, step := range []struct {
		action string
		item   string
		want   int64
	}{
		{"read", items[0], 2},
		{"read", items[1], 0},
		{"unread", items[0], 1},
		{"unread", items[0], 0},
		{"archive", items[1], 3},
		{"archive", items[0], 0},
		{"unarchive", items[2], 3},
		{"unarchive", items[2], 0},
	} {
		e := endpoint(step.action)
		var resp struct {
			ID    string `json:"id"`
			Count *int64 `json:"count"`
		}
		testutil.Call(t, serveInboxEndpoint(e.handler), e.request(f, step.item, token)).Want(http.StatusOK).JSON(&resp)
		if resp.ID != step.item || resp.Count == nil || *resp.Count != step.want {
			t.Fatalf("%s %s: id=%s count=%v, want %d", step.action, step.item, resp.ID, resp.Count, step.want)
		}
	}
}

func TestDeleteInboxItemIsHumanOnly(t *testing.T) {
	f := newInboxWriteFixture(t)
	endpoint := inboxOwnerEndpoint{method: http.MethodDelete, path: func(item, _ string) string { return "/api/inbox/" + item }, param: "id"}

	item := f.item(t)
	testutil.Call(t, serveInboxEndpoint((*Handler).DeleteInboxItem), endpoint.request(f, item, f.taskToken(t, testUserID))).Want(http.StatusForbidden)
	requireInboxItemUntouched(t, item)

	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": testUserID, "name": "Inbox delete test", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12]})
	testutil.Call(t, serveInboxEndpoint((*Handler).DeleteInboxItem), endpoint.request(f, item, pat)).Want(http.StatusOK)
	var remaining int
	dbfx.QueryRow(t, `SELECT count(*) FROM inbox_item WHERE id = $1`, item).Scan(&remaining)
	if remaining != 0 {
		t.Fatal("human delete left the notification in place")
	}
}

// A task credential is bound to one workspace. When the owner triggered it
// and belongs to other workspaces too, the cross-workspace summary must not
// carry those workspaces' IDs or unread counts back to the agent, while the
// owner's own credential still sees every workspace. The inbox read guard
// coalesces concurrent summary requests per user, so the agent and the owner
// are lined up to share one query, in both arrival orders: whichever of them
// starts the shared query, each gets only what its credential allows.
func TestUnreadInboxSummaryTaskCredentialStaysInBoundWorkspace(t *testing.T) {
	f := newInboxWriteFixture(t)
	other := newInboxWriteFixture(t)
	f.item(t)
	other.item(t)
	token := f.taskToken(t, testUserID)

	for _, agentFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("agent first=%v", agentFirst), func(t *testing.T) {
			gate := make(chan struct{})
			starter := &gatedSummaryStarter{inner: testPool, gate: gate, blocked: make(chan struct{})}
			h := *testHandler
			h.InboxReads = inboxread.New(inboxread.Config{Concurrency: 2})
			h.TxStarter = starter
			asAgent := middleware.Auth(h.Queries, nil, nil, nil)(
				middleware.RequireWorkspaceMember(h.Queries)(http.HandlerFunc(h.UnreadInboxSummary)))
			asOwner := middleware.RequireWorkspaceMember(h.Queries)(http.HandlerFunc(h.UnreadInboxSummary))
			agentRequest := func() *http.Request {
				return testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, "/api/inbox/unread-summary", nil),
					"Authorization", "Bearer "+token, "X-Workspace-ID", f.workspaceID)
			}
			ownerRequest := func() *http.Request {
				return inboxRequest(http.MethodGet, "/api/inbox/unread-summary", f.workspaceID)
			}
			serve := func(handler http.Handler, req *http.Request) <-chan *httptest.ResponseRecorder {
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					done <- rec
				}()
				return done
			}
			// joined waits until n callers are inside the pending follow-up
			// generation, counted under the guard's admission lock.
			joined := func(n int) {
				t.Helper()
				deadline := time.Now().Add(2 * time.Second)
				for h.InboxReads.PendingSummaryCallers(testUserID) != n {
					if time.Now().After(deadline) {
						t.Fatalf("pending summary callers = %d, want %d", h.InboxReads.PendingSummaryCallers(testUserID), n)
					}
					time.Sleep(time.Millisecond)
				}
			}

			// An owner request occupies the running query, so the next two
			// requests queue into one shared follow-up query.
			warmUp := serve(asOwner, ownerRequest())
			select {
			case <-starter.blocked:
			case rec := <-warmUp:
				t.Fatalf("warm-up summary finished before its query was held: %d %s", rec.Code, rec.Body.String())
			case <-time.After(5 * time.Second):
				t.Fatal("warm-up summary query never started")
			}
			var agentDone, ownerDone <-chan *httptest.ResponseRecorder
			if agentFirst {
				agentDone = serve(asAgent, agentRequest())
				joined(1)
				ownerDone = serve(asOwner, ownerRequest())
			} else {
				ownerDone = serve(asOwner, ownerRequest())
				joined(1)
				agentDone = serve(asAgent, agentRequest())
			}
			joined(2)
			close(gate)

			summaryOf := func(name string, done <-chan *httptest.ResponseRecorder) map[string]int64 {
				t.Helper()
				rec := <-done
				if rec.Code != http.StatusOK {
					t.Fatalf("%s summary status = %d: %s", name, rec.Code, rec.Body.String())
				}
				var rows []InboxWorkspaceUnreadResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
					t.Fatalf("%s summary: %v", name, err)
				}
				counts := make(map[string]int64, len(rows))
				for _, row := range rows {
					counts[row.WorkspaceID] = row.Count
				}
				return counts
			}
			agentView := summaryOf("agent", agentDone)
			if len(agentView) != 1 || agentView[f.workspaceID] != 1 {
				t.Fatalf("task credential summary = %v; want only bound workspace %s with 1 unread", agentView, f.workspaceID)
			}
			for name, done := range map[string]<-chan *httptest.ResponseRecorder{"owner": ownerDone, "warm-up owner": warmUp} {
				if view := summaryOf(name, done); view[f.workspaceID] != 1 || view[other.workspaceID] != 1 {
					t.Fatalf("%s summary = %v; want both workspaces", name, view)
				}
			}
			if queries := starter.queries.Load(); queries != 2 {
				t.Fatalf("summary queries = %d; want 2, the agent and owner sharing one", queries)
			}
		})
	}
}

// gatedSummaryStarter holds every inbox read query until gate closes, and
// counts them. Begin itself is not held: it runs under a short setup budget.
type gatedSummaryStarter struct {
	inner   txStarter
	gate    <-chan struct{}
	blocked chan struct{}
	once    sync.Once
	queries atomic.Int32
}

func (s *gatedSummaryStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return gatedSummaryTx{Tx: tx, starter: s}, nil
}

type gatedSummaryTx struct {
	pgx.Tx
	starter *gatedSummaryStarter
}

func (tx gatedSummaryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.starter.queries.Add(1)
	tx.starter.once.Do(func() { close(tx.starter.blocked) })
	select {
	case <-tx.starter.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return tx.Tx.Query(ctx, sql, args...)
}

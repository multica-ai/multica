package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *appsFixture) submitSend(t *testing.T) AppRequestResponse {
	t.Helper()
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}})
	_, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{"to": "x@example.com"}})
	if isErr || !strings.HasPrefix(response, "submitted") {
		t.Fatalf("submit: %s", response)
	}
	requests := f.listRequests(t, testUserID)
	if len(requests) != 1 {
		t.Fatalf("requests: %d", len(requests))
	}
	return requests[0]
}

func (f *appsFixture) requestStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM app_action_request WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// A run's credential acts as its owner, but must not approve its own calls,
// rewrite the owner's rules, or read the owner's requests.
func TestConnectedApps_RunCredentialIsNotAPerson(t *testing.T) {
	f := newAppsFixture(t)
	req := f.submitSend(t)
	token := "mat_apps_boundary_" + f.taskID
	if _, err := f.h.Queries.CreateTaskToken(context.Background(), db.CreateTaskTokenParams{
		TokenHash: auth.HashToken(token), TaskID: parseUUID(f.taskID), AgentID: parseUUID(f.agentID),
		WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.h.Queries.DeleteTaskTokensByTask(context.Background(), parseUUID(f.taskID)) })

	router := chi.NewRouter()
	router.Use(middleware.Auth(f.h.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(f.h.Queries))
	router.Post("/api/app-requests/{id}/approve", f.h.ApproveAppRequest)
	router.Get("/api/app-requests", f.h.ListAppRequests)
	router.Put("/api/apps/rules", f.h.UpdateAppRules)
	send := func(method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}

	if code := send(http.MethodPost, "/api/app-requests/"+req.ID+"/approve", ""); code != http.StatusForbidden {
		t.Errorf("run credential approved its own call: HTTP %d, executions=%d", code, len(f.sdk.executions()))
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Errorf("executions = %d, want 0", n)
	}
	if code := send(http.MethodPut, "/api/apps/rules?toolkit_slug=gmail&scope=personal", `{"rules":[{"action_class":"send","policy":"allow"}]}`); code != http.StatusForbidden {
		t.Errorf("run credential changed the owner's send rule: HTTP %d", code)
	}
	if code := send(http.MethodGet, "/api/app-requests?issue_id="+f.issueID, ""); code != http.StatusForbidden {
		t.Errorf("run credential read the owner's requests: HTTP %d", code)
	}
	if got := f.requestStatus(t, req.ID); got != "pending" {
		t.Errorf("request status = %s, want pending", got)
	}
}

// Leaving the workspace withdraws what waited on the member; connecting the
// app afterwards resumes nothing there.
func TestConnectedApps_DepartedMemberCannotResumeOnConnect(t *testing.T) {
	f := newAppsFixture(t)
	ctx := context.Background()
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.member, f.taskID); err != nil {
		t.Fatal(err)
	}
	_, response, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "gmail"})
	if isErr || !strings.HasPrefix(response, "requested") {
		t.Fatalf("request: %s", response)
	}
	var requestID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM app_action_request WHERE task_id=$1 AND kind='connect'`, f.taskID).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, f.taskID); err != nil {
		t.Fatal(err)
	}
	member, err := f.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(f.member), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.revokeAndRemoveMember(ctx, member.WorkspaceID, member.UserID, member.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	if got := f.requestStatus(t, requestID); got != "cancelled" {
		t.Errorf("request after the member left = %s, want cancelled", got)
	}
	accountID := fmt.Sprintf("ca_departed_%s", f.member)
	if _, err := f.h.Queries.UpsertUserComposioConnection(ctx, db.UpsertUserComposioConnectionParams{UserID: member.UserID, ToolkitSlug: "gmail", AuthConfigID: "ac_gmail", ConnectedAccountID: accountID, ComposioUserID: f.member}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM user_composio_connection WHERE connected_account_id=$1`, accountID)
	})
	f.h.ConnectedApps.ResolveConnectRequests(ctx, member.UserID, "gmail")
	var queued int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='queued' AND originator_user_id=$2`, f.issueID, f.member).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Errorf("OAuth completion queued %d runs in a workspace the member already left", queued)
	}
}

// Access to the agent is checked again when deciding: a request whose
// approver lost it is withdrawn instead of run.
func TestConnectedApps_ApprovalRechecksAgentAccess(t *testing.T) {
	f := newAppsFixture(t)
	ctx := context.Background()
	req := f.submitSend(t)
	if !f.h.CanActFor(ctx, parseUUID(testWorkspaceID), parseUUID(f.agentID), parseUUID(testUserID)) {
		t.Fatal("the agent's owner should be able to act for it")
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, f.agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `UPDATE agent SET archived_at = NULL WHERE id = $1`, f.agentID) })
	w := httptest.NewRecorder()
	f.h.ApproveAppRequest(w, withURLParam(appsReqAs(t, testUserID, http.MethodPost, "/api/app-requests/"+req.ID+"/approve", nil), "id", req.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("approve for an archived agent: %d %s", w.Code, w.Body.String())
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("executions = %d, want 0", n)
	}
	if got := f.requestStatus(t, req.ID); got != "cancelled" {
		t.Fatalf("request = %s, want cancelled", got)
	}
}

// Approving shares the account's per-minute budget with direct calls; an
// approval over budget leaves the request pending to approve again.
func TestConnectedApps_ApprovedCallsRespectRateLimit(t *testing.T) {
	f := newAppsFixture(t)
	req := f.submitSend(t)
	if _, err := testPool.Exec(context.Background(), `INSERT INTO app_tool_call (workspace_id, agent_id, account_scope, account_owner_user_id, toolkit_slug, tool_slug, action_class, outcome) SELECT $1,$2,'personal',$3,'gmail','GMAIL_FETCH_EMAILS','read','ok' FROM generate_series(1,30)`, testWorkspaceID, f.agentID, testUserID); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.h.ApproveAppRequest(w, withURLParam(appsReqAs(t, testUserID, http.MethodPost, "/api/app-requests/"+req.ID+"/approve", nil), "id", req.ID))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("approve over budget: %d %s", w.Code, w.Body.String())
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("approval executed %d provider calls over the account's budget", n)
	}
	if got := f.requestStatus(t, req.ID); got != "pending" {
		t.Fatalf("request after a rate-limited approval = %s, want pending", got)
	}
}

const notionUpdatePage = "NOTION_UPDATE_PAGE"

// withNotionDeleteBlocked attaches Notion at level with the personal account's
// delete rule set to block. NOTION_UPDATE_PAGE also takes archived=true, which
// moves the page to trash.
func (f *appsFixture) withNotionDeleteBlocked(t *testing.T, level string) {
	t.Helper()
	ctx := context.Background()
	old := appsFakeGmailTools
	appsFakeGmailTools = append(append([]sdk.Tool(nil), old...), sdk.Tool{
		Slug: notionUpdatePage, Name: "Update Page", Toolkit: sdk.Toolkit{Slug: "notion"},
		Description: "Update page properties, icon, cover, or archive status.",
	})
	t.Cleanup(func() { appsFakeGmailTools = old })
	account := "ca_notion_" + f.taskID
	if _, err := f.h.Queries.UpsertUserComposioConnection(ctx, db.UpsertUserComposioConnectionParams{
		UserID: parseUUID(testUserID), ToolkitSlug: "notion", AuthConfigID: "ac_notion",
		ConnectedAccountID: account, ComposioUserID: testUserID,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM user_composio_connection WHERE connected_account_id=$1`, account)
	})
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "notion", "account_scope": "personal", "level": level}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	f.h.UpdateAppRules(w, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/rules?toolkit_slug=notion&scope=personal", map[string]any{
		"rules": []map[string]string{{"action_class": "delete", "policy": "block"}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("set delete rule: %d %s", w.Code, w.Body.String())
	}
}

// Archiving through an update tool is a delete and obeys the delete rule;
// ordinary edits and restores through the same tool still run.
func TestConnectedApps_ArchiveViaUpdateObeysDeleteRule(t *testing.T) {
	f := newAppsFixture(t)
	f.withNotionDeleteBlocked(t, "write")
	run := func(args map[string]any) (bool, string) {
		_, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": notionUpdatePage, "arguments": args})
		return isErr, response
	}
	if isErr, response := run(map[string]any{"page_id": "p1", "archived": true}); !isErr || len(f.sdk.executions()) != 0 {
		t.Fatalf("archive bypassed delete=block: isError=%v executions=%d response=%s", isErr, len(f.sdk.executions()), response)
	}
	if isErr, response := run(map[string]any{"page_id": "p1", "in_trash": "true"}); !isErr || len(f.sdk.executions()) != 0 {
		t.Fatalf("in_trash bypassed delete=block: isError=%v response=%s", isErr, response)
	}
	if isErr, response := run(map[string]any{"page_id": "p1", "properties": map[string]any{"title": "Q3"}}); isErr {
		t.Fatalf("plain edit refused: %s", response)
	}
	if isErr, response := run(map[string]any{"page_id": "p1", "archived": false}); isErr {
		t.Fatalf("restore refused: %s", response)
	}
	if n := len(f.sdk.executions()); n != 2 {
		t.Fatalf("executions = %d, want the edit and the restore", n)
	}
}

// An approver who edits a confirmed update into an archive gets the delete
// rule, not the class the request was submitted under.
func TestConnectedApps_ApprovalWithEditedArchiveObeysDeleteRule(t *testing.T) {
	f := newAppsFixture(t)
	f.withNotionDeleteBlocked(t, "confirm_writes")
	_, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{
		"tool": notionUpdatePage, "arguments": map[string]any{"page_id": "p1", "properties": map[string]any{"title": "Q3"}},
	})
	if isErr || !strings.HasPrefix(response, "submitted") {
		t.Fatalf("submit: %s", response)
	}
	requests := f.listRequests(t, testUserID)
	if len(requests) != 1 {
		t.Fatalf("requests: %d", len(requests))
	}
	id := requests[0].ID
	w := httptest.NewRecorder()
	f.h.ApproveAppRequest(w, withURLParam(appsReqAs(t, testUserID, http.MethodPost, "/api/app-requests/"+id+"/approve", map[string]any{
		"arguments": map[string]any{"page_id": "p1", "archived": true},
	}), "id", id))
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("edited archive ran despite delete=block: HTTP %d executions=%d %s", w.Code, n, w.Body.String())
	}
	if got := f.requestStatus(t, id); got != "failed" {
		t.Fatalf("request = %s, want failed", got)
	}
}

// A long decided history never pushes a pending request out of the list;
// the history pages back through `before`.
func TestConnectedApps_PendingVisibleAfterHistoricalRequests(t *testing.T) {
	f := newAppsFixture(t)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO app_action_request (workspace_id, task_id, agent_id, issue_id, approver_user_id, account_scope, toolkit_slug, status, expires_at, created_at)
		SELECT $1, $2, $3, $4, $5, 'personal', 'gmail', 'rejected', now()+interval '1 day', now()-interval '1 day'+(g * interval '1 second') FROM generate_series(1,200) g
	`, testWorkspaceID, f.taskID, f.agentID, f.issueID, testUserID); err != nil {
		t.Fatal(err)
	}
	_, response, isErr := f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_SEND_EMAIL", "arguments": map[string]any{"to": "x@example.com"}})
	if isErr || !strings.HasPrefix(response, "submitted") {
		t.Fatalf("submit: %s", response)
	}

	type page struct {
		Requests   []AppRequestResponse `json:"requests"`
		NextBefore *string              `json:"next_before"`
	}
	list := func(query string) page {
		w := httptest.NewRecorder()
		f.h.ListAppRequests(w, appsReqAs(t, testUserID, http.MethodGet, "/api/app-requests?issue_id="+f.issueID+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		var p page
		json.Unmarshal(w.Body.Bytes(), &p)
		return p
	}
	first := list("")
	pending, decided := 0, 0
	for _, r := range first.Requests {
		if r.Status == "pending" {
			pending++
		} else {
			decided++
		}
	}
	if pending != 1 || decided != appRequestPageSize || first.NextBefore == nil {
		t.Fatalf("first page: pending=%d decided=%d next=%v", pending, decided, first.NextBefore)
	}
	seen := decided
	cursor := *first.NextBefore
	for cursor != "" {
		p := list("&before=" + cursor)
		for _, r := range p.Requests {
			if r.Status == "pending" {
				t.Fatal("a history page repeated a pending request")
			}
		}
		seen += len(p.Requests)
		cursor = ""
		if p.NextBefore != nil {
			cursor = *p.NextBefore
		}
	}
	if seen != 200 {
		t.Fatalf("history pages carried %d decided requests, want 200", seen)
	}
}

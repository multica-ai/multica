package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	pa "github.com/multica-ai/multica/server/internal/integrations/prautomation"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func policyFixture(t *testing.T) (pgtype.UUID, string, int64) {
	t.Helper()
	ws := dbfx.Workspace(t, "PR policy", "pr-policy-"+uuid.NewString(), testutil.Cols{"issue_prefix": "POL"})
	dbfx.Member(t, ws, testUserID, "owner")
	id := dbfx.Issue(t, "Deliver", testutil.Cols{"workspace_id": ws, "number": 1, "status": "in_review"})
	inst := time.Now().UnixNano()
	dbfx.Insert(t, "github_installation", testutil.Cols{"workspace_id": ws, "installation_id": inst, "account_login": "policy", "account_type": "User"})
	t.Cleanup(func() {
		for _, table := range []string{"pr_automation_connection", "pr_automation_override", "pr_automation_issue", "pr_automation_evidence", "pr_automation_policy", "activity_log"} {
			testPool.Exec(context.Background(), "DELETE FROM "+table+" WHERE workspace_id=$1", ws)
		}
		testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id=$1)`, ws)
		testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE workspace_id=$1`, ws)
	})
	return parseUUID(ws), id, inst
}
func policyEvent(t *testing.T, ws pgtype.UUID, inst int64, n int32, title, body, state string, at int) {
	t.Helper()
	p := ghPullRequestPayload{}
	p.Action = "edited"
	p.Installation.ID = inst
	p.Repository.Name = "repo"
	p.Repository.Owner.Login = "policy"
	p.PullRequest.Number = n
	p.PullRequest.Title = title
	p.PullRequest.Body = body
	p.PullRequest.State = state
	if state == "merged" {
		p.PullRequest.State = "closed"
		p.PullRequest.Merged = true
		p.Action = "closed"
		p.PullRequest.MergedAt = fmt.Sprintf("2026-09-16T08:00:%02dZ", at)
	}
	p.PullRequest.HTMLURL = fmt.Sprintf("https://github.com/policy/repo/pull/%d", n)
	p.PullRequest.CreatedAt = "2026-09-16T08:00:00Z"
	p.PullRequest.UpdatedAt = fmt.Sprintf("2026-09-16T08:00:%02dZ", at)
	testHandler.mirrorPullRequestForWorkspace(context.Background(), ws, inst, &p, closeIntentPolicy{unrestricted: true})
}
func setTestPolicy(t *testing.T, ws pgtype.UUID, source string, complete bool) {
	t.Helper()
	dbfx.Exec(t, `INSERT INTO pr_automation_policy(workspace_id,source,auto_complete) VALUES($1,$2,$3)`, ws, source, complete)
}
func wantPolicyStatus(t *testing.T, id, status string) {
	t.Helper()
	i, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(id))
	if err != nil || i.Status != status {
		t.Fatalf("status=%s err=%v want=%s", i.Status, err, status)
	}
}

func TestPRPolicyNoKeywordAndAllMerged(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", true)
	policyEvent(t, ws, inst, 1, "POL-1: first", "", "open", 1)
	policyEvent(t, ws, inst, 2, "POL-1: second", "", "open", 2)
	policyEvent(t, ws, inst, 1, "POL-1: first", "", "merged", 3)
	wantPolicyStatus(t, id, "in_review")
	policyEvent(t, ws, inst, 2, "POL-1: second", "", "closed", 4)
	wantPolicyStatus(t, id, "in_review")
	policyEvent(t, ws, inst, 2, "POL-1: second", "", "merged", 5)
	wantPolicyStatus(t, id, "done")
	// Old metadata cannot resurrect a merged PR or change its links.
	policyEvent(t, ws, inst, 1, "unrelated", "", "open", 1)
	var state string
	if err := testPool.QueryRow(context.Background(), `SELECT state FROM github_pull_request WHERE workspace_id=$1 AND pr_number=1`, ws).Scan(&state); err != nil || state != "merged" {
		t.Fatalf("state=%s err=%v", state, err)
	}
	// Duplicate delivery creates no second completion activity.
	policyEvent(t, ws, inst, 2, "POL-1: second", "", "merged", 5)
	var count int
	dbfxQueryErr := testPool.QueryRow(context.Background(), `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND details->>'source'='pr_automation'`, id).Scan(&count)
	if dbfxQueryErr != nil || count != 1 {
		t.Fatalf("activities=%d err=%v", count, dbfxQueryErr)
	}
}
func TestPRPolicyLateLinkAndReopen(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", true)
	policyEvent(t, ws, inst, 1, "unrelated", "Closes POL-1", "merged", 1)
	wantPolicyStatus(t, id, "in_review")
	policyEvent(t, ws, inst, 1, "POL-1: repaired", "", "merged", 2)
	wantPolicyStatus(t, id, "done")
	if _, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{WorkspaceID: ws, ID: parseUUID(id), Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if err := testHandler.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	wantPolicyStatus(t, id, "in_progress")
	var disabled bool
	if err := testPool.QueryRow(context.Background(), `SELECT disabled FROM pr_automation_issue WHERE issue_id=$1`, id).Scan(&disabled); err != nil || !disabled {
		t.Fatalf("disabled=%v err=%v", disabled, err)
	}
}
func TestPRPolicyReconcilesCompleteRemovalAndExclusions(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", false)
	policyEvent(t, ws, inst, 1, "POL-1", "", "open", 1)
	policyEvent(t, ws, inst, 1, "no identifier", "", "open", 2)
	links, err := testHandler.Queries.ListPullRequestsByIssue(context.Background(), parseUUID(id))
	if err != nil || len(links) != 0 {
		t.Fatalf("links=%d err=%v", len(links), err)
	}
	policyEvent(t, ws, inst, 1, "POL-1", "", "open", 3)
	links, err = testHandler.Queries.ListPullRequestsByIssue(context.Background(), parseUUID(id))
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%d err=%v", len(links), err)
	}
	dbfx.Exec(t, `INSERT INTO pr_automation_override(issue_id,pr_id,workspace_id,mode) VALUES($1,$2,$3,'excluded')`, id, links[0].ID, ws)
	policyEvent(t, ws, inst, 1, "POL-1", "", "open", 4)
	links, err = testHandler.Queries.ListPullRequestsByIssue(context.Background(), parseUUID(id))
	if err != nil || len(links) != 0 {
		t.Fatalf("excluded links=%d err=%v", len(links), err)
	}
	plan, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, nil, false)
	if err != nil || len(plan.Issues[0].Excluded) != 1 {
		t.Fatalf("exclusion lost: %+v %v", plan, err)
	}
}
func TestPRPolicyMigrationPreviewAndToken(t *testing.T) {
	ws, id, inst := policyFixture(t)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	wantPolicyStatus(t, id, "in_review")
	proposed := pa.Policy{Source: "title_branch", AutoComplete: true}
	a, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, &proposed, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Migrated || !a.Issues[0].Decision.Complete {
		t.Fatalf("preview=%+v", a)
	}
	b, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, &proposed, false)
	if err != nil || a.Token != b.Token {
		t.Fatal("unstable preview", err)
	}
	dbfx.Exec(t, `UPDATE issue SET title='changed',revision=revision+1 WHERE id=$1`, id)
	b, err = testHandler.prPolicyPlan(context.Background(), testPool, ws, &proposed, false)
	if err != nil || a.Token == b.Token {
		t.Fatal("preview did not expire", err)
	}
	wantPolicyStatus(t, id, "in_review")
}

func policyRequest(t *testing.T, method, path, ws, user string, body any) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	router.Get("/workspaces/{id}", testHandler.GetPRPolicy)
	router.Post("/workspaces/{id}/preview", testHandler.PreviewPRPolicy)
	router.Post("/workspaces/{id}/sync", testHandler.SyncPRPolicy)
	router.Put("/workspaces/{id}", testHandler.UpdatePRPolicy)
	router.Get("/issues/{id}", testHandler.GetIssuePRPolicy)
	router.Put("/issues/{id}", testHandler.UpdateIssuePRPolicy)
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("X-Workspace-ID", ws)
	req.Header.Set("X-User-ID", user)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestPRPolicyHTTPPreviewApplyAndPermissions(t *testing.T) {
	ws, id, inst := policyFixture(t)
	wss := uuidToString(ws)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	req := map[string]any{"source": "title_branch", "auto_complete": true}
	call := func(method, suffix string, body any) *httptest.ResponseRecorder {
		return policyRequest(t, method, "/workspaces/"+wss+suffix, wss, testUserID, body)
	}
	rec := call("POST", "/preview", req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var preview prPolicyPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	wantPolicyStatus(t, id, "in_review")
	req["token"] = preview.Token
	// A PR changed after preview; the apply must make no policy/status writes.
	policyEvent(t, ws, inst, 2, "POL-1 second", "", "open", 2)
	rec = call("PUT", "", req)
	if rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	_, migrated, err := readPRPolicy(context.Background(), testPool, ws)
	if err != nil || migrated {
		t.Fatal("stale preview migrated workspace", err)
	}
	rec = call("POST", "/preview", req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &preview)
	req["token"] = preview.Token
	rec = call("PUT", "", req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	wantPolicyStatus(t, id, "in_review")
	// Apply is a one-shot token, not an unguarded repeatable write.
	if rec = call("PUT", "", req); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	member := dbfx.User(t, "PR member", "pr-member-"+uuid.NewString()+"@test.invalid")
	dbfx.Member(t, wss, member, "member")
	if rec = policyRequest(t, "POST", "/workspaces/"+wss+"/preview", wss, member, req); rec.Code != 403 {
		t.Fatal("member policy write", rec.Code)
	}
	outsider := dbfx.User(t, "PR outsider", "pr-outsider-"+uuid.NewString()+"@test.invalid")
	if rec = policyRequest(t, "GET", "/workspaces/"+wss, wss, outsider, nil); rec.Code != 404 {
		t.Fatal("outsider policy read", rec.Code)
	}
}
func TestPRPolicyManualOverridesAndTenantScope(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "manual", true)
	policyEvent(t, ws, inst, 1, "unrelated", "", "merged", 1)
	var pid string
	dbfx.QueryRow(t, `SELECT id::text FROM github_pull_request WHERE workspace_id=$1`, ws).Scan(&pid)
	call := func(body any) *httptest.ResponseRecorder {
		return policyRequest(t, "PUT", "/issues/"+id, uuidToString(ws), testUserID, body)
	}
	rec := call(map[string]any{"pr_id": pid, "mode": "manual"})
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	wantPolicyStatus(t, id, "done")
	dbfx.Exec(t, `UPDATE issue SET status='in_review' WHERE id=$1`, id)
	rec = call(map[string]any{"disabled": false})
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	wantPolicyStatus(t, id, "done")
	foreign, _, otherInst := policyFixture(t)
	policyEvent(t, foreign, otherInst, 1, "unrelated", "", "open", 1)
	dbfx.QueryRow(t, `SELECT id::text FROM github_pull_request WHERE workspace_id=$1`, foreign).Scan(&pid)
	rec = call(map[string]any{"pr_id": pid, "mode": "manual"})
	if rec.Code != 404 {
		t.Fatal("cross-workspace link", rec.Code, rec.Body.String())
	}
}
func TestPRPolicyAmbiguousAutomaticManualResolves(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", true)
	other, _, _ := policyFixture(t)
	dbfx.Insert(t, "github_installation", testutil.Cols{"workspace_id": other, "installation_id": inst, "account_login": "shared", "account_type": "User"})
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	plan, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Issues[0].Decision.Reason != "ambiguous" || len(plan.Issues[0].Links) != 0 {
		t.Fatalf("%+v", plan)
	}
	wantPolicyStatus(t, id, "in_review")
	var pid string
	dbfx.QueryRow(t, `SELECT id::text FROM github_pull_request WHERE workspace_id=$1`, ws).Scan(&pid)
	rec := policyRequest(t, "PUT", "/issues/"+id, uuidToString(ws), testUserID, map[string]any{"pr_id": pid, "mode": "manual"})
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	wantPolicyStatus(t, id, "done")
}
func TestPRPolicyUnknownBodyBlocksAndTriageAcceptance(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "all", true)
	dbfx.Exec(t, `UPDATE issue SET triage_state='pending' WHERE id=$1`, id)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	policyEvent(t, ws, inst, 2, "other", "", "open", 2)
	dbfx.Exec(t, `DELETE FROM pr_automation_evidence WHERE pr_id IN (SELECT id FROM github_pull_request WHERE workspace_id=$1 AND pr_number=2)`, ws)
	dbfx.Exec(t, `UPDATE issue SET triage_state=NULL WHERE id=$1`, id)
	if err := testHandler.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	wantPolicyStatus(t, id, "in_review")
	policyEvent(t, ws, inst, 2, "other", "", "open", 2)
	wantPolicyStatus(t, id, "done")
}

func TestPRPolicyCrossProviderDisconnectAndReconnect(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", true)
	h := *testHandler
	h.cfg.VCSIntegrationEnabled = true
	connParams := db.UpsertVCSConnectionParams{WorkspaceID: ws, Provider: "forgejo", InstanceUrl: "https://git.example.test", AccountLogin: "policy", AccessTokenEncrypted: "sealed", WebhookSecretEncrypted: "sealed"}
	conn, err := h.Queries.UpsertVCSConnection(context.Background(), connParams)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_vcs_pull_request WHERE issue_id=$1`, id)
		testPool.Exec(context.Background(), `DELETE FROM vcs_pull_request WHERE workspace_id=$1`, ws)
		testPool.Exec(context.Background(), `DELETE FROM vcs_connection WHERE workspace_id=$1`, ws)
	})
	ev := vcs.PullRequestEvent{Action: "opened", RepoOwner: "policy", RepoName: "repo", Number: 1, Title: "POL-1", State: "open", HTMLURL: "https://git.example.test/policy/repo/pulls/1", CreatedAt: "2026-09-16T08:00:01Z", UpdatedAt: "2026-09-16T08:00:01Z"}
	h.mirrorVCSPullRequest(context.Background(), conn, ev)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 2)
	wantPolicyStatus(t, id, "in_review")
	if err = h.Queries.DeleteVCSConnection(context.Background(), db.DeleteVCSConnectionParams{ID: conn.ID, WorkspaceID: ws}); err != nil {
		t.Fatal(err)
	}
	if err = h.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	wantPolicyStatus(t, id, "in_review")
	plan, err := h.prPolicyPlan(context.Background(), testPool, ws, nil, false)
	if err != nil || len(plan.Issues[0].Links) != 2 || plan.Issues[0].Decision.Reason != "sync_required" {
		t.Fatalf("lost disconnected PR: %+v err=%v", plan, err)
	}
	restored, err := h.Queries.UpsertVCSConnection(context.Background(), connParams)
	if err != nil || restored.ID != conn.ID {
		t.Fatal("reconnect lost PR identity", err)
	}
	ev.Action = "closed"
	ev.State = "merged"
	ev.UpdatedAt = "2026-09-16T08:00:03Z"
	h.mirrorVCSPullRequest(context.Background(), restored, ev)
	wantPolicyStatus(t, id, "done")
}
func TestPRPolicyURLScopes(t *testing.T) {
	for _, raw := range []string{"https://github.com.evil.test/a/b/pull/1", "https://user:secret@github.com/a/b/pull/1", "https://github.com/a/../pull/1", "https://github.com/a/b/pull/1?token=x", "file:///a/b/pull/1", "https://github.com/a/b/pull/2147483648"} {
		if _, _, _, err := prPolicyURL(raw, "https://github.com", "github"); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	owner, repo, n, err := prPolicyURL("https://git.example.test/root/group/sub/repo/-/merge_requests/12", "https://git.example.test/root", "gitlab")
	if err != nil || owner != "group/sub" || repo != "repo" || n != 12 {
		t.Fatal(owner, repo, n, err)
	}
}
func TestPRPolicyProviderFetchAndRedirect(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.EscapedPath()
		if r.Header.Get("PRIVATE-TOKEN") != "token" {
			t.Error("missing provider auth")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"iid":12,"title":"POL-1","description":"ordinary POL-2","state":"merged","web_url":"https://git.example.test/g/sub/repo/-/merge_requests/12","source_branch":"feature","sha":"head","created_at":"2026-09-16T08:00:00Z","updated_at":"2026-09-16T09:00:00Z"}`)
	}))
	defer server.Close()
	ev, err := fetchVCSPolicyPR(context.Background(), db.VcsConnection{InstanceUrl: server.URL, Provider: "gitlab"}, "token", "g/sub", "repo", 12)
	if err != nil || ev.State != "merged" || ev.Body != "ordinary POL-2" || seenPath != "/api/v4/projects/g%2Fsub%2Frepo/merge_requests/12" {
		t.Fatal(ev, seenPath, err)
	}
	var leaked bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	_, err = fetchVCSPolicyPR(context.Background(), db.VcsConnection{InstanceUrl: redirect.URL, Provider: "forgejo"}, "secret", "g", "repo", 12)
	if err == nil || leaked {
		t.Fatal("followed authenticated redirect")
	}
}

func TestPRPolicySameTimestampConflictRequiresAuthoritativeSync(t *testing.T) {
	ws, id, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", true)
	policyEvent(t, ws, inst, 1, "POL-1", "", "open", 1)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	wantPolicyStatus(t, id, "in_review")
	plan, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, nil, false)
	if err != nil || plan.Issues[0].Decision.Reason != "sync_required" {
		t.Fatalf("conflict not visible: %+v %v", plan, err)
	}
	p := ghPullRequestPayload{}
	p.Action = "synchronized"
	p.Repository.Name = "repo"
	p.Repository.Owner.Login = "policy"
	p.Installation.ID = inst
	p.PullRequest.Number = 1
	p.PullRequest.Title = "POL-1"
	p.PullRequest.State = "closed"
	p.PullRequest.Merged = true
	p.PullRequest.HTMLURL = "https://github.com/policy/repo/pull/1"
	p.PullRequest.CreatedAt = "2026-09-16T08:00:00Z"
	p.PullRequest.UpdatedAt = "2026-09-16T08:00:01Z"
	if err = testHandler.mirrorPullRequestForWorkspace(context.Background(), ws, inst, &p, closeIntentPolicy{}); err != nil {
		t.Fatal(err)
	}
	wantPolicyStatus(t, id, "done")
}
func TestPRPolicyBoundedProgressRechecksDisabledPolicy(t *testing.T) {
	ws, _, inst := policyFixture(t)
	setTestPolicy(t, ws, "title_branch", false)
	title := "POL-1"
	for n := 2; n <= 101; n++ {
		dbfx.Issue(t, "Batch", testutil.Cols{"workspace_id": ws, "number": n, "status": "in_review"})
		title += fmt.Sprintf(" POL-%d", n)
	}
	policyEvent(t, ws, inst, 1, title, "", "merged", 1)
	dbfx.Exec(t, `UPDATE pr_automation_policy SET auto_complete=true,revision=revision+1 WHERE workspace_id=$1`, ws)
	if err := testHandler.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	var done int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND status='done'`, ws).Scan(&done)
	if done != 100 {
		t.Fatalf("unbounded write count=%d", done)
	}
	dbfx.Exec(t, `UPDATE pr_automation_policy SET auto_complete=false,revision=revision+1 WHERE workspace_id=$1`, ws)
	if err := testHandler.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND status='done'`, ws).Scan(&done)
	if done != 100 {
		t.Fatal("disabled policy drained stale completion", done)
	}
	dbfx.Exec(t, `UPDATE pr_automation_policy SET auto_complete=true,revision=revision+1 WHERE workspace_id=$1`, ws)
	if err := testHandler.ReconcilePRPolicy(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND status='done'`, ws).Scan(&done)
	if done != 101 {
		t.Fatal("did not resume durable progress", done)
	}
}

func TestPRPolicyUsesOneConnectionDuringIngestion(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		t.Run(fmt.Sprint(migrated), func(t *testing.T) {
			ws, id, inst := policyFixture(t)
			if migrated {
				setTestPolicy(t, ws, "title_branch", true)
			}
			cfg := testPool.Config()
			cfg.MaxConns = 1
			cfg.MinConns = 0
			pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			h := *testHandler
			h.DB = pool
			h.TxStarter = pool
			h.Queries = db.New(pool)
			h.IssueStatusCatalog = nil
			p := ghPullRequestPayload{}
			p.Action = "closed"
			p.Repository.Name = "repo"
			p.Repository.Owner.Login = "policy"
			p.Installation.ID = inst
			p.PullRequest.Number = 1
			p.PullRequest.Title = "POL-1"
			p.PullRequest.Body = "Closes POL-1"
			p.PullRequest.State = "closed"
			p.PullRequest.Merged = true
			p.PullRequest.HTMLURL = "https://github.com/policy/repo/pull/1"
			p.PullRequest.CreatedAt = "2026-09-16T08:00:00Z"
			p.PullRequest.UpdatedAt = "2026-09-16T08:00:01Z"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err = h.mirrorPullRequestForWorkspace(ctx, ws, inst, &p, closeIntentPolicy{unrestricted: true}); err != nil {
				t.Fatal("nested pool acquisition", err)
			}
			wantPolicyStatus(t, id, "done")
		})
	}
}

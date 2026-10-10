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

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGongfengRepositoryImportAndSnapshot(t *testing.T) {
	ctx := context.Background()
	withVCSBox(t)
	issueID := dbfx.Issue(t, "Import an existing Gongfeng MR", testutil.Cols{"status": "in_progress"})
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE issue_id = $1", issueID)
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := testHandler.Queries.GetWorkspace(ctx, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	identifier := fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
	sha := "head-one"
	state := "opened"
	stamp := "2026-10-09T08:00:00+0000"
	denied := false
	hookID := int64(0)
	hookToken := ""
	hookPosts := 0
	var connID string
	project := map[string]any{"id": 55, "path_with_namespace": "acme/team/widget", "web_url": "https://git.code.tencent.com/acme/team/widget", "ssh_url_to_repo": "git@git.code.tencent.com:acme/team/widget.git", "default_branch": "main"}
	mr := func() map[string]any {
		return map[string]any{"id": 901, "iid": 7, "title": identifier + " Fix login", "description": "Closes " + identifier, "state": state, "source_project_id": 88, "source_branch": "feature/login", "created_at": "2026-10-09T07:00:00+0000", "updated_at": stamp, "merge_status": "can_be_merged", "author": map[string]any{"username": "alice"}}
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "api-token" {
			t.Error("wrong access token")
		}
		if denied {
			w.WriteHeader(403)
			_, _ = w.Write([]byte("api-token must not leak"))
			return
		}
		write := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.URL.Path == "/api/v3/projects":
			write([]any{project})
		case r.URL.Path == "/api/v3/projects/55" || r.URL.Path == "/api/v3/projects/acme/team/widget":
			write(project)
		case r.URL.Path == "/api/v3/projects/55/hooks" && r.Method == "GET":
			if hookID == 0 {
				write([]any{})
			} else {
				write([]any{map[string]any{"id": hookID, "url": testHandler.vcsWebhookURL(connID)}})
			}
		case r.URL.Path == "/api/v3/projects/55/hooks" && r.Method == "POST" || r.URL.Path == "/api/v3/projects/55/hooks/9" && r.Method == "PUT":
			var hook struct {
				Token string `json:"token"`
				Merge bool   `json:"merge_requests_events"`
				SSL   bool   `json:"enable_ssl_verification"`
			}
			if err := json.NewDecoder(r.Body).Decode(&hook); err != nil {
				t.Error(err)
			}
			if !hook.Merge || !hook.SSL {
				t.Error("webhook must enable MR events and TLS validation")
			}
			hookToken = hook.Token
			if r.Method == "POST" {
				hookPosts++
			}
			hookID = 9
			write(map[string]any{"id": 9})
		case r.URL.Path == "/api/v3/projects/55/merge_requests":
			write([]any{mr()})
		case r.URL.Path == "/api/v3/projects/55/merge_request/901":
			write(mr())
		case r.URL.Path == "/api/v3/projects/88/repository/branches/feature/login":
			write(map[string]any{"commit": map[string]any{"id": sha}})
		case r.URL.Path == "/api/v3/projects/88/commits/"+sha+"/statuses":
			write([]any{map[string]any{"id": 1, "context": "build", "state": "success"}, map[string]any{"id": 2, "context": "unit", "state": "failure"}})
		default:
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	// The test drives the worker synchronously; it never starts its goroutines.
	tokenEnc, _ := testHandler.sealVCSSecret("api-token")
	secretEnc, _ := testHandler.sealVCSSecret("hook-secret")
	connID = dbfx.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": testWorkspaceID, "provider": "gongfeng", "instance_url": provider.URL, "access_token_encrypted": tokenEnc, "webhook_secret_encrypted": secretEnc, "account_login": "alice"})
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteVCSConnection(ctx, db.DeleteVCSConnectionParams{ID: parseUUID(connID), WorkspaceID: parseUUID(testWorkspaceID)})
	})
	oldPublic := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://multica.example"
	t.Cleanup(func() { testHandler.cfg.PublicURL = oldPublic })
	req := testutil.WithURLParams(newRequest("POST", "/repositories/55", nil), "id", testWorkspaceID, "connectionId", connID, "projectId", "55")
	var selected GongfengRepositoryResponse
	testutil.Call(t, testHandler.AddGongfengRepository, req).Want(200).JSON(&selected)
	if selected.ID != 55 || !selected.Syncing {
		t.Fatalf("selected %+v", selected)
	}
	row, err := testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
	if err != nil {
		t.Fatal(err)
	}
	testHandler.GongfengSync.syncRepository(ctx, row)
	row, _ = testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
	if row.SyncError != "" || !row.SyncedAt.Valid || row.HookID != 9 || hookToken != "hook-secret" {
		t.Fatalf("sync %+v, token %q", row, hookToken)
	}
	prs, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issueID))
	if err != nil || len(prs) != 1 {
		t.Fatalf("import linked %d PRs, err %v", len(prs), err)
	}
	pr, err := testHandler.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: prs[0].ID, WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	testHandler.GongfengSync.refreshPR(ctx, pr)
	response := listIssuePRsForTest(t, issueID).PullRequests[0]
	if response.SnapshotAvailable == nil || !*response.SnapshotAvailable || response.ChecksTotal != 2 || response.ChecksFailed != 1 || response.Mergeable == nil || *response.Mergeable != "mergeable" || len(response.FailedCheckNames) != 1 {
		t.Fatalf("snapshot %+v", response)
	}
	// A newer commit immediately invalidates the old CI snapshot, even before refresh.
	sha = "head-two"
	stamp = "2026-10-09T08:10:00+0000"
	_, err = testPool.Exec(ctx, "UPDATE vcs_pull_request SET head_sha=$2, pr_updated_at=$3 WHERE id=$1", uuidToString(pr.ID), sha, parseGHTimeRequired("2026-10-09T08:10:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	response = listIssuePRsForTest(t, issueID).PullRequests[0]
	if response.SnapshotAvailable == nil || *response.SnapshotAvailable || response.ChecksTotal != 0 {
		t.Fatalf("old checks leaked onto new head %+v", response)
	}
	changed, _ := testHandler.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: pr.ID, WorkspaceID: pr.WorkspaceID})
	count, err := testHandler.Queries.SaveGongfengSnapshot(ctx, db.SaveGongfengSnapshotParams{ID: pr.ID, SnapshotHeadSha: "head-one", Snapshot: []byte(`{"total":1,"passed":1}`), PrUpdatedAt: pr.PrUpdatedAt, UpdatedAt: row.UpdatedAt})
	if err != nil || count != 0 {
		t.Fatalf("stale snapshot accepted: %d %v", count, err)
	}
	testHandler.GongfengSync.refreshPR(ctx, changed)
	response = listIssuePRsForTest(t, issueID).PullRequests[0]
	if response.SnapshotAvailable == nil || !*response.SnapshotAvailable {
		t.Fatalf("new checks not fetched %+v", response)
	}
	denied = true
	testHandler.GongfengSync.refreshPR(ctx, changed)
	response = listIssuePRsForTest(t, issueID).PullRequests[0]
	if !response.SnapshotStale {
		t.Fatal("failed refresh must mark last-known checks stale")
	}
	retryReq := testutil.WithURLParams(newRequest("POST", "/repositories/55/sync", nil), "id", testWorkspaceID, "connectionId", connID, "projectId", "55")
	testutil.Call(t, testHandler.RetryGongfengRepository, retryReq).Want(200)
	row, _ = testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
	testHandler.GongfengSync.syncRepository(ctx, row)
	row, _ = testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
	if !strings.Contains(row.SyncError, "permission") || strings.Contains(row.SyncError, "api-token") {
		t.Fatalf("unsafe/missing sync failure %q", row.SyncError)
	}
	denied = false
	var rotated VCSConnectResponse
	testutil.Call(t, testHandler.RotateVCSConnectionWebhook, vcsHandlerRequest("POST", "/rotate-webhook", nil, connID)).Want(200).JSON(&rotated)
	row, _ = testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
	testHandler.GongfengSync.syncRepository(ctx, row)
	if hookPosts != 1 || hookToken != rotated.WebhookSecret {
		t.Fatalf("rotation did not update existing hook, posts %d", hookPosts)
	}
	state = "merged"
	stamp = "2026-10-09T08:20:00+0000"
	changed, _ = testHandler.Queries.GetVCSPullRequestInWorkspace(ctx, db.GetVCSPullRequestInWorkspaceParams{ID: pr.ID, WorkspaceID: pr.WorkspaceID})
	testHandler.GongfengSync.refreshPR(ctx, changed)
	issue, _ = testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if issue.Status != "done" {
		t.Fatalf("API merge did not complete issue: %s", issue.Status)
	}
	// Tenant and deployment guards run before making requests to the provider.
	wrongWS := dbfx.Workspace(t, "Other tenant", "gongfeng-other-tenant")
	wrongReq := testutil.WithURLParams(newRequest("GET", "/repositories", nil), "id", wrongWS, "connectionId", connID)
	testutil.Call(t, testHandler.ListGongfengRepositories, wrongReq).Want(404)
	testHandler.cfg.VCSIntegrationEnabled = false
	testutil.Call(t, testHandler.ListGongfengRepositories, vcsHandlerRequest("GET", "/repositories", nil, connID)).Want(404)
}

func TestGongfengSnapshotAvailability(t *testing.T) {
	// A stored older snapshot may have passing checks; no current head means no verdict.
	row := db.ListVCSPullRequestsByIssueRow{Provider: "gongfeng", HeadSha: "new", SnapshotHeadSha: "old", Snapshot: []byte(`{"total":1,"passed":1,"rollup":"success"}`), SnapshotFetchedAt: parseGHTimeRequired(time.Now().UTC().Format(time.RFC3339Nano))}
	resp := vcsPullRequestRowToResponse(row)
	if resp.SnapshotAvailable == nil || *resp.SnapshotAvailable || resp.ChecksRollup != nil || resp.ChecksPassed != 0 {
		t.Fatalf("stale head snapshot %+v", resp)
	}
}

func TestGongfengHistoryPaginationResumesAfterFailure(t *testing.T) {
	ctx := context.Background()
	withVCSBox(t)
	failPageTwo := true
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/hooks") {
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`[]`))
			} else {
				_, _ = w.Write([]byte(`{"id":9}`))
			}
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/merge_requests") {
			http.NotFound(w, r)
			return
		}
		page := r.URL.Query().Get("page")
		if page == "2" && failPageTwo {
			w.WriteHeader(429)
			return
		}
		start, end := 1, 20
		if page == "2" {
			start, end = 21, 21
		} else {
			w.Header().Set("X-Next-Page", "2")
		}
		mrs := make([]map[string]any, 0, end-start+1)
		for n := start; n <= end; n++ {
			mrs = append(mrs, map[string]any{"id": 1000 + n, "iid": n, "title": "Historical MR", "state": "closed", "source_branch": "old", "created_at": "2026-10-09T07:00:00+0000", "updated_at": "2026-10-09T08:00:00+0000"})
		}
		_ = json.NewEncoder(w).Encode(mrs)
	}))
	defer provider.Close()
	tokenEnc, _ := testHandler.sealVCSSecret("api-token")
	secretEnc, _ := testHandler.sealVCSSecret("hook-secret")
	connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": testWorkspaceID, "provider": "gongfeng", "instance_url": provider.URL, "access_token_encrypted": tokenEnc, "webhook_secret_encrypted": secretEnc, "account_login": "alice"})
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteVCSConnection(ctx, db.DeleteVCSConnectionParams{ID: parseUUID(connID), WorkspaceID: parseUUID(testWorkspaceID)})
	})
	oldPublic := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://multica.example"
	t.Cleanup(func() { testHandler.cfg.PublicURL = oldPublic })
	row, err := testHandler.Queries.AddGongfengRepository(ctx, db.AddGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55, Path: "acme/widget", WebUrl: "https://git.code.tencent.com/acme/widget", CloneUrl: "git@git.code.tencent.com:acme/widget.git"})
	if err != nil {
		t.Fatal(err)
	}
	load := func() db.GongfengRepository {
		t.Helper()
		row, err := testHandler.Queries.GetGongfengRepository(ctx, db.GetGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	testHandler.GongfengSync.syncRepository(ctx, row)
	row = load()
	if row.NextPage != 2 || row.SyncedAt.Valid || !row.ScanStartedAt.Valid {
		t.Fatalf("first page cursor %+v", row)
	}
	// A fresh worker instance resumes the stored cursor rather than starting over.
	worker := newGongfengSync(testHandler)
	worker.syncRepository(ctx, row)
	row = load()
	if row.NextPage != 2 || !strings.Contains(row.SyncError, "rate limit") {
		t.Fatalf("failed page progress %+v", row)
	}
	failPageTwo = false
	worker.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	worker.syncRepository(ctx, row)
	row = load()
	if row.NextPage != 1 || !row.SyncedAt.Valid || row.ScanStartedAt.Valid || row.SyncError != "" {
		t.Fatalf("completed import %+v", row)
	}
	var count int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM vcs_pull_request WHERE connection_id=$1", connID).Scan(&count); err != nil || count != 21 {
		t.Fatalf("history count %d, err %v", count, err)
	}
	// A subsequent scan ignores entries older than the completed watermark.
	worker.syncRepository(ctx, row)
	row = load()
	if row.NextPage != 1 || !row.SyncedAt.Valid || row.SyncError != "" {
		t.Fatalf("incremental scan %+v", row)
	}
}

func TestGongfengDisconnectWaitsForImportAndRemovesItsData(t *testing.T) {
	ctx := context.Background()
	withVCSBox(t)
	started, release, workerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var connID string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/hooks") && r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/hooks") && r.Method == "POST":
			_, _ = w.Write([]byte(`{"id":9}`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			close(started)
			<-release
			_, _ = w.Write([]byte(`[{"id":901,"iid":7,"title":"Historical MR","state":"closed","created_at":"2026-10-09T07:00:00+0000","updated_at":"2026-10-09T08:00:00+0000"}]`))
		case strings.HasSuffix(r.URL.Path, "/hooks/9") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "url": testHandler.vcsWebhookURL(connID)})
		case strings.HasSuffix(r.URL.Path, "/hooks/9") && r.Method == "DELETE":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	tokenEnc, _ := testHandler.sealVCSSecret("api-token")
	secretEnc, _ := testHandler.sealVCSSecret("hook-secret")
	connID = dbfx.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": testWorkspaceID, "provider": "gongfeng", "instance_url": provider.URL, "access_token_encrypted": tokenEnc, "webhook_secret_encrypted": secretEnc, "account_login": "alice"})
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteVCSConnection(ctx, db.DeleteVCSConnectionParams{ID: parseUUID(connID), WorkspaceID: parseUUID(testWorkspaceID)})
	})
	oldPublic := testHandler.cfg.PublicURL
	testHandler.cfg.PublicURL = "https://multica.example"
	t.Cleanup(func() { testHandler.cfg.PublicURL = oldPublic })
	row, err := testHandler.Queries.AddGongfengRepository(ctx, db.AddGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55, Path: "acme/widget", WebUrl: "https://git.code.tencent.com/acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	go func() { testHandler.GongfengSync.syncRepository(ctx, row); close(workerDone) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("import did not start")
	}
	deleted := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		testHandler.DeleteVCSConnection(w, vcsHandlerRequest("DELETE", "/connection", nil, connID))
		deleted <- w
	}()
	select {
	case w := <-deleted:
		close(release)
		t.Fatalf("disconnect bypassed an active import: %d", w.Code)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-workerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("import did not finish")
	}
	select {
	case w := <-deleted:
		if w.Code != 204 {
			t.Fatalf("disconnect %d: %s", w.Code, w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not finish")
	}
	for _, table := range []string{"gongfeng_repository", "vcs_pull_request"} {
		var count int
		if err := testPool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE connection_id=$1", connID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s rows %d, err %v", table, count, err)
		}
	}
	// A queued pre-disconnect job sees the removed connection and cannot resurrect data.
	testHandler.GongfengSync.syncRepository(ctx, row)
}

func TestGongfengDisconnectRevokesLocallyWithExpiredToken(t *testing.T) {
	ctx := context.Background()
	withVCSBox(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("api-token is invalid"))
	}))
	defer provider.Close()
	tokenEnc, _ := testHandler.sealVCSSecret("api-token")
	secretEnc, _ := testHandler.sealVCSSecret("hook-secret")
	connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": testWorkspaceID, "provider": "gongfeng", "instance_url": provider.URL, "access_token_encrypted": tokenEnc, "webhook_secret_encrypted": secretEnc, "account_login": "alice"})
	_, err := testHandler.Queries.AddGongfengRepository(ctx, db.AddGongfengRepositoryParams{ConnectionID: parseUUID(connID), ProjectID: 55, Path: "acme/widget", WebUrl: "https://git.code.tencent.com/acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = testPool.Exec(ctx, "UPDATE gongfeng_repository SET hook_id=9 WHERE connection_id=$1", connID)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error string `json:"webhook_cleanup_error"`
	}
	testutil.Call(t, testHandler.DeleteVCSConnection, vcsHandlerRequest("DELETE", "/connection", nil, connID)).Want(200).JSON(&response)
	if response.Error == "" || strings.Contains(response.Error, "api-token") {
		t.Fatalf("missing or unsafe cleanup warning %q", response.Error)
	}
	var count int
	err = testPool.QueryRow(ctx, "SELECT count(*) FROM vcs_connection WHERE id=$1", connID).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("expired-token connection retained: count %d, err %v", count, err)
	}
	testutil.Call(t, testHandler.DeleteVCSConnection, vcsHandlerRequest("DELETE", "/connection", nil, connID)).Want(204)
}

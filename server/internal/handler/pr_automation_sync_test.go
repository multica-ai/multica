package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type prPolicyTransport func(*http.Request) (*http.Response, error)

func (f prPolicyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func policyProviderFixture(t *testing.T, ws pgtype.UUID, provider, title, state string, onRead ...func()) string {
	t.Helper()
	read := func() {
		for _, f := range onRead {
			f()
		}
	}
	if provider == "github" {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GITHUB_APP_ID", "test-app")
		t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
		var inst int64
		if err := testPool.QueryRow(context.Background(), `SELECT installation_id FROM github_installation WHERE workspace_id=$1`, ws).Scan(&inst); err != nil {
			t.Fatal(err)
		}
		original := http.DefaultTransport
		t.Cleanup(func() { http.DefaultTransport = original })
		http.DefaultTransport = prPolicyTransport(func(r *http.Request) (*http.Response, error) {
			w := httptest.NewRecorder()
			if r.URL.Host != "api.github.com" {
				return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
			}
			switch r.URL.Path {
			case fmt.Sprintf("/app/installations/%d/access_tokens", inst):
				json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
			case "/repos/policy/repo/pulls/2":
				read()
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing installation credential")
				}
				json.NewEncoder(w).Encode(map[string]any{
					"number": 2, "title": title, "body": "Closes POL-1", "state": state, "merged": state == "closed",
					"html_url": "https://github.com/policy/repo/pull/2", "created_at": "2026-09-16T08:00:00Z", "updated_at": "2026-09-16T09:00:00Z",
				})
			default:
				return nil, fmt.Errorf("unexpected provider path %s", r.URL.Path)
			}
			return w.Result(), nil
		})
		return "https://github.com/policy/repo/pull/2"
	}
	withVCSBox(t)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/policy/repo/pulls/2" || r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		read()
		json.NewEncoder(w).Encode(map[string]any{
			"number": 2, "title": title, "body": "Closes POL-1", "state": state, "merged": state == "closed",
			"html_url": origin + "/policy/repo/pulls/2", "created_at": "2026-09-16T08:00:00Z", "updated_at": "2026-09-16T09:00:00Z",
		})
	}))
	origin = server.URL
	t.Cleanup(server.Close)
	sealed, err := testHandler.sealVCSSecret("test-token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = testHandler.Queries.UpsertVCSConnection(context.Background(), db.UpsertVCSConnectionParams{
		WorkspaceID: ws, Provider: "forgejo", InstanceUrl: origin, AccountLogin: "policy", AccessTokenEncrypted: sealed, WebhookSecretEncrypted: sealed,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_vcs_pull_request WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id=$1)`, ws)
		testPool.Exec(context.Background(), `DELETE FROM vcs_pull_request WHERE workspace_id=$1`, ws)
		testPool.Exec(context.Background(), `DELETE FROM vcs_connection WHERE workspace_id=$1`, ws)
	})
	return origin + "/policy/repo/pulls/2"
}

func TestPRPolicyManualURLDoesNotCompleteBeforeLink(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			ws, id, inst := policyFixture(t)
			other := dbfx.Issue(t, "Unrelated deliverable", testutil.Cols{"workspace_id": ws, "number": 2, "status": "in_review"})
			setTestPolicy(t, ws, "title_branch", false)
			policyEvent(t, ws, inst, 1, "POL-1 POL-2", "", "merged", 1)
			// Both issues are eligible on the next reconciliation. The local action
			// must first attach the open PR and must not complete unrelated issues.
			dbfx.Exec(t, `UPDATE pr_automation_policy SET auto_complete=true WHERE workspace_id=$1`, ws)
			url := policyProviderFixture(t, ws, provider, "Additional work", "open")
			rec := policyRequest(t, "PUT", "/issues/"+id, uuidToString(ws), testUserID, map[string]any{"mode": "manual", "url": url})
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			wantPolicyStatus(t, id, "in_review")
			wantPolicyStatus(t, other, "in_review")
			var links, completions int
			dbfx.QueryRow(t, `SELECT count(*) FROM (SELECT issue_id FROM issue_pull_request UNION ALL SELECT issue_id FROM issue_vcs_pull_request) l WHERE issue_id=$1`, id).Scan(&links)
			dbfx.QueryRow(t, `SELECT count(*) FROM activity_log WHERE workspace_id=$1 AND details->>'source'='pr_automation'`, ws).Scan(&completions)
			if links != 2 || completions != 0 {
				t.Fatalf("links=%d premature completions=%d", links, completions)
			}
		})
	}
}

func TestPRPolicyLegacySyncOnlyStoresMetadata(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			ws, id, _ := policyFixture(t)
			url := policyProviderFixture(t, ws, provider, "Closes POL-1", "closed")
			if err := testHandler.syncPRPolicyURL(context.Background(), ws, url); err != nil {
				t.Fatal(err)
			}
			wantPolicyStatus(t, id, "in_review")
			var links, evidence int
			dbfx.QueryRow(t, `SELECT count(*) FROM (SELECT issue_id FROM issue_pull_request UNION ALL SELECT issue_id FROM issue_vcs_pull_request) l WHERE issue_id=$1`, id).Scan(&links)
			dbfx.QueryRow(t, `SELECT count(*) FROM pr_automation_evidence WHERE workspace_id=$1 AND body='Closes POL-1'`, ws).Scan(&evidence)
			if links != 0 || evidence != 1 {
				t.Fatalf("links=%d evidence=%d", links, evidence)
			}
		})
	}
}

func TestPRPolicyAllAmbiguityOnlyBlocksMatchingIssue(t *testing.T) {
	ws, id, inst := policyFixture(t)
	otherWS, _, _ := policyFixture(t)
	dbfx.Insert(t, "github_installation", testutil.Cols{"workspace_id": otherWS, "installation_id": inst, "account_login": "shared", "account_type": "User"})
	unambiguous := dbfx.Issue(t, "Unambiguous", testutil.Cols{"workspace_id": ws, "number": 2, "status": "in_review"})
	setTestPolicy(t, ws, "all", true)
	policyEvent(t, ws, inst, 1, "POL-1", "", "merged", 1)
	policyEvent(t, ws, inst, 2, "POL-2", "", "merged", 2)
	wantPolicyStatus(t, id, "in_review")
	wantPolicyStatus(t, unambiguous, "done")
	plan, err := testHandler.prPolicyPlan(context.Background(), testPool, ws, nil, false)
	if err != nil || len(plan.Pending) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestPRPolicyManualURLRollsBackOnDisconnect(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			ws, id, _ := policyFixture(t)
			setTestPolicy(t, ws, "title_branch", true)
			url := policyProviderFixture(t, ws, provider, "Closes POL-1", "closed", func() {
				// The provider was authorized at fetch time but disconnected before
				// the local write. Neither metadata nor a link may survive rejection.
				table := "github_installation"
				if provider != "github" {
					table = "vcs_connection"
				}
				if _, err := testPool.Exec(context.Background(), "DELETE FROM "+table+" WHERE workspace_id=$1", ws); err != nil {
					t.Error(err)
				}
			})
			rec := policyRequest(t, "PUT", "/issues/"+id, uuidToString(ws), testUserID, map[string]any{"mode": "manual", "url": url})
			if rec.Code != 409 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			wantPolicyStatus(t, id, "in_review")
			for _, table := range []string{"github_pull_request", "vcs_pull_request", "pr_automation_override", "pr_automation_evidence", "activity_log"} {
				var count int
				if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", ws).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial write in %s: %d err=%v", table, count, err)
				}
			}
		})
	}
}

func TestPRPolicyManualMergedURLCompletesOnce(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			ws, id, _ := policyFixture(t)
			setTestPolicy(t, ws, "manual", true)
			url := policyProviderFixture(t, ws, provider, "Additional work", "closed")
			for attempt := 0; attempt < 2; attempt++ {
				rec := policyRequest(t, "PUT", "/issues/"+id, uuidToString(ws), testUserID, map[string]any{"mode": "manual", "url": url})
				if rec.Code != 200 {
					t.Fatal(rec.Code, rec.Body.String())
				}
			}
			wantPolicyStatus(t, id, "done")
			var completions int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM activity_log WHERE issue_id=$1 AND details->>'source'='pr_automation'`, id).Scan(&completions); err != nil || completions != 1 {
				t.Fatalf("completions=%d err=%v", completions, err)
			}
		})
	}
}

func TestPRPolicyLegacySyncDoesNotCompleteExistingLink(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		t.Run(provider, func(t *testing.T) {
			ws, id, inst := policyFixture(t)
			url := policyProviderFixture(t, ws, provider, "Closes POL-1", "closed")
			snapshot, err := testHandler.fetchPRPolicyURL(context.Background(), ws, url)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.github != nil {
				p := snapshot.github
				p.Action, p.PullRequest.State, p.PullRequest.Merged = "opened", "open", false
				p.PullRequest.UpdatedAt = "2026-09-16T08:00:00Z"
				err = testHandler.mirrorPullRequestForWorkspace(context.Background(), ws, inst, p, closeIntentPolicy{unrestricted: true})
			} else {
				ev := snapshot.vcs
				ev.Action, ev.State, ev.UpdatedAt = "opened", "open", "2026-09-16T08:00:00Z"
				err = testHandler.mirrorVCSPullRequest(context.Background(), snapshot.connection, ev)
			}
			if err != nil {
				t.Fatal(err)
			}
			// The Settings sync endpoint must not act on this existing close intent.
			dbfx.Exec(t, `DELETE FROM pr_automation_evidence WHERE workspace_id=$1`, ws)
			rec := policyRequest(t, "POST", "/workspaces/"+uuidToString(ws)+"/sync", uuidToString(ws), testUserID, nil)
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			wantPolicyStatus(t, id, "in_review")
			var state string
			if err := testPool.QueryRow(context.Background(), `SELECT state FROM (SELECT workspace_id,state FROM github_pull_request UNION ALL SELECT workspace_id,state FROM vcs_pull_request) p WHERE workspace_id=$1`, ws).Scan(&state); err != nil || state != "merged" {
				t.Fatalf("metadata not refreshed: state=%s err=%v", state, err)
			}
		})
	}
}

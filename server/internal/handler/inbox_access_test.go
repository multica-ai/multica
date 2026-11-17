package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func inboxTaskCredential(t *testing.T, taskID, agentID, workspaceID, userID string) string {
	t.Helper()
	token := "mat_" + uuid.NewString()
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID,
		"workspace_id": workspaceID, "user_id": userID, "expires_at": time.Now().Add(time.Hour),
	})
	return token
}

// Every Inbox read that returns the owner's notifications is gated by
// requireInboxOwnerAccess; the access matrix runs against each of them.
type inboxOwnerRead struct {
	name, path, query string
	handler           func(*Handler, http.ResponseWriter, *http.Request)
}

var inboxOwnerReads = []inboxOwnerRead{
	{"active", "/api/inbox", "ListInboxItems", (*Handler).ListInbox},
	{"archived", "/api/inbox/archived", "ListArchivedInboxItems", (*Handler).ListArchivedInbox},
	{"archived page", "/api/inbox/archived/page", "ListArchivedInboxPage", (*Handler).ListArchivedInboxPage},
	{"archived facets", "/api/inbox/archived/facets", "ArchivedInboxFacets", (*Handler).GetArchivedInboxFacets},
}

func (e inboxOwnerRead) serve(h *Handler, cloud *auth.CloudPATVerifier) http.HandlerFunc {
	return middleware.Auth(testHandler.Queries, nil, cloud, nil)(
		middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			e.handler(h, w, r)
		})),
	).ServeHTTP
}

func (e inboxOwnerRead) request(token, workspaceID string) *http.Request {
	return testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, e.path, nil),
		"Authorization", "Bearer "+token, "X-Workspace-ID", workspaceID)
}

type inboxAccessFailureDB struct {
	db.DBTX
	query                string
	err                  error
	failures, inboxReads int
}

func (d *inboxAccessFailureDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: "+d.query+" :one\n") {
		d.failures++
		return &mockRow{err: d.err}
	}
	return d.DBTX.QueryRow(ctx, sql, args...)
}

func (d *inboxAccessFailureDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	for _, e := range inboxOwnerReads {
		if strings.HasPrefix(sql, "-- name: "+e.query+" :many\n") {
			d.inboxReads++
		}
	}
	return d.DBTX.Query(ctx, sql, args...)
}

func TestListInboxAccessLookupErrors(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Inbox lookup runtime")
	agentID := dbfx.Agent(t, "Inbox lookup agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	token := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)
	const privateDetails = "private-inbox-database-host-and-query"
	for _, e := range inboxOwnerReads {
		for _, query := range []string{"GetAgentTask", "GetAgentRuntimeForWorkspace"} {
			for _, tc := range []struct {
				name   string
				err    error
				status int
			}{
				{"missing", pgx.ErrNoRows, http.StatusForbidden},
				{"wrapped missing", fmt.Errorf("%s: %w", privateDetails, pgx.ErrNoRows), http.StatusForbidden},
				{"deadline", fmt.Errorf("%s: %w", privateDetails, context.DeadlineExceeded), http.StatusServiceUnavailable},
				{"statement timeout", &pgconn.PgError{Code: "57014", Message: privateDetails}, http.StatusServiceUnavailable},
				{"connection failure", errors.New(privateDetails), http.StatusInternalServerError},
			} {
				t.Run(e.name+"/"+query+"/"+tc.name, func(t *testing.T) {
					faults := &inboxAccessFailureDB{DBTX: testPool, query: query, err: tc.err}
					h := *testHandler
					h.Queries = db.New(faults)
					response := testutil.Call(t, e.serve(&h, nil), e.request(token, testWorkspaceID)).Want(tc.status)
					if tc.status == http.StatusServiceUnavailable && response.Header().Get("Retry-After") != "1" {
						t.Fatal("temporary access-lookup failure must supply Retry-After")
					}
					body := response.Map()
					if faults.failures != 1 || faults.inboxReads != 0 {
						t.Fatalf("lookup failures=%d inbox reads=%d; want one failure and no Inbox read", faults.failures, faults.inboxReads)
					}
					publicError, ok := body["error"].(string)
					if len(body) != 1 || !ok || publicError == "" || strings.Contains(fmt.Sprint(body), privateDetails) {
						t.Fatalf("response must contain only a protected public error: %v", body)
					}
					if tc.status == http.StatusInternalServerError && body["error"] != "failed to resolve inbox access" {
						t.Fatalf("service failure reported as an authorization error: %v", body)
					}
				})
			}
		}
	}
}

func inboxAccessHandler(cloud *auth.CloudPATVerifier) http.HandlerFunc {
	return inboxOwnerReads[0].serve(testHandler, cloud)
}

func inboxCredentialRequest(token, workspaceID string) *http.Request {
	return testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, "/api/inbox", nil),
		"Authorization", "Bearer "+token, "X-Workspace-ID", workspaceID)
}

func TestListInboxTaskCredentialAccess(t *testing.T) {
	other := createPlainMember(t, "inbox-other-"+uuid.NewString()+"@example.test")
	foreignWorkspace := dbfx.Workspace(t, "Foreign inbox", "inbox-foreign-"+uuid.NewString())
	dbfx.Member(t, foreignWorkspace, testUserID, "owner")
	for _, tc := range []struct {
		name       string
		originator any
		status     string
		change     string
		want       int
	}{
		{"self-triggered", testUserID, "running", "", http.StatusOK},
		{"another human", other, "running", "", http.StatusForbidden},
		{"unattributed despite accountable owner", nil, "running", "", http.StatusForbidden},
		{"completed task with lingering token", testUserID, "completed", "", http.StatusForbidden},
		{"failed task with lingering token", testUserID, "failed", "", http.StatusForbidden},
		{"cancelled task with lingering token", testUserID, "cancelled", "", http.StatusForbidden},
		{"runtime ownership changed", testUserID, "running", "owner", http.StatusForbidden},
		{"missing runtime owner", testUserID, "running", "no-owner", http.StatusForbidden},
		{"runtime in another workspace", testUserID, "running", "workspace", http.StatusForbidden},
		{"task agent differs from token", testUserID, "running", "agent", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Inbox runtime")
			agentID := dbfx.Agent(t, "Inbox agent", runtimeID)
			accountable := tc.originator
			if accountable == nil {
				accountable = testUserID
			}
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "originator_user_id": tc.originator,
				"accountable_user_id": accountable, "status": tc.status,
			})
			switch tc.change {
			case "owner":
				dbfx.Exec(t, `UPDATE agent_runtime SET owner_id=$2 WHERE id=$1`, runtimeID, other)
			case "no-owner":
				dbfx.Exec(t, `UPDATE agent_runtime SET owner_id=NULL WHERE id=$1`, runtimeID)
			case "workspace":
				dbfx.Exec(t, `UPDATE agent_runtime SET workspace_id=$2 WHERE id=$1`, runtimeID, foreignWorkspace)
			case "agent":
				agentID = dbfx.Agent(t, "Token agent", runtimeID)
			}
			token := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)
			for _, e := range inboxOwnerReads {
				t.Run(e.name, func(t *testing.T) {
					testutil.Call(t, e.serve(testHandler, nil), e.request(token, testWorkspaceID)).Want(tc.want)
				})
			}
		})
	}
}

func TestListInboxRejectsForgedTaskIdentityAndWorkspace(t *testing.T) {
	other := createPlainMember(t, "inbox-forger-"+uuid.NewString()+"@example.test")
	runtimeID := dbfx.Runtime(t, "Inbox runtime")
	agentID := dbfx.Agent(t, "Inbox agent", runtimeID)
	allowedTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	deniedTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running", "originator_user_id": other, "accountable_user_id": other})
	token := inboxTaskCredential(t, deniedTask, agentID, testWorkspaceID, testUserID)
	foreignWorkspace := dbfx.Workspace(t, "Foreign inbox", "inbox-forged-ws-"+uuid.NewString())
	dbfx.Member(t, foreignWorkspace, testUserID, "owner")
	allowedToken := inboxTaskCredential(t, allowedTask, agentID, testWorkspaceID, testUserID)
	for _, e := range inboxOwnerReads {
		t.Run(e.name, func(t *testing.T) {
			req := testutil.WithHeaders(e.request(token, testWorkspaceID), "X-Task-ID", allowedTask, "X-Agent-ID", agentID,
				"X-Actor-Source", "member", "X-User-ID", other, "X-Originator-User-ID", testUserID)
			req.URL.RawQuery = "originator_user_id=" + testUserID + "&recipient_id=" + other
			testutil.Call(t, e.serve(testHandler, nil), req).Want(http.StatusForbidden)

			testutil.Call(t, e.serve(testHandler, nil), e.request(allowedToken, foreignWorkspace)).Want(http.StatusForbidden)
		})
	}
}

func TestListInboxCloudPATCannotBorrowTaskIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "owner_id": testUserID})
	}))
	defer srv.Close()
	verifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{FleetBaseURL: srv.URL})
	runtimeID := dbfx.Runtime(t, "Cloud inbox runtime")
	agentID := dbfx.Agent(t, "Cloud inbox agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	for _, e := range inboxOwnerReads {
		t.Run(e.name, func(t *testing.T) {
			req := testutil.WithHeaders(e.request("mcn_inbox-test", testWorkspaceID),
				"X-Actor-Source", "task_token", "X-Task-ID", taskID, "X-Agent-ID", agentID)
			testutil.Call(t, e.serve(testHandler, verifier), req).Want(http.StatusForbidden)
		})
	}
}

func TestListInboxHumanCredentialsAndReadOnlyIsolation(t *testing.T) {
	other := createPlainMember(t, "inbox-recipient-"+uuid.NewString()+"@example.test")
	foreignWorkspace := dbfx.Workspace(t, "Foreign inbox", "inbox-isolation-"+uuid.NewString())
	dbfx.Member(t, foreignWorkspace, testUserID, "owner")
	newItem := func(workspaceID, recipientID string) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{"workspace_id": workspaceID,
			"recipient_type": "member", "recipient_id": recipientID, "type": "system", "severity": "info", "title": "Private personal notice"})
	}
	own := newItem(testWorkspaceID, testUserID)
	otherItem := newItem(testWorkspaceID, other)
	foreign := newItem(foreignWorkspace, testUserID)
	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": testUserID, "name": "Inbox test", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12]})
	jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": testUserID, "exp": time.Now().Add(time.Hour).Unix()}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatal(err)
	}
	runtimeID := dbfx.Runtime(t, "Own inbox runtime")
	agentID := dbfx.Agent(t, "Own inbox agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	taskToken := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)
	for _, tc := range []struct {
		name, token string
		cookie      bool
	}{
		{"personal PAT", pat, false}, {"login session", jwtToken, true}, {"self-triggered task", taskToken, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := inboxCredentialRequest(tc.token, testWorkspaceID)
			if tc.cookie {
				req.Header.Del("Authorization")
				req.AddCookie(&http.Cookie{Name: auth.AuthCookieName, Value: tc.token})
			}
			req.Header.Set("X-User-ID", other)
			req.URL.RawQuery = "recipient_id=" + other
			var items []InboxItemResponse
			testutil.Call(t, inboxAccessHandler(nil), req).Want(http.StatusOK).JSON(&items)
			found := false
			for _, item := range items {
				if item.ID == otherItem || item.ID == foreign {
					t.Fatalf("another recipient or workspace leaked: %s", item.ID)
				}
				if item.ID == own {
					found = true
					if item.Read || item.Archived {
						t.Fatal("read changed notification state")
					}
				}
			}
			if !found {
				t.Fatal("own personal notification missing")
			}
			if state := loadInboxItemState(t, own); state != (inboxItemState{}) {
				t.Fatalf("GET mutated state: %+v", state)
			}
		})
	}
}

func TestListInboxDelegatedOriginatorAndProjectVisibility(t *testing.T) {
	var counterBefore int64
	dbfx.QueryRow(t, `SELECT GREATEST(issue_counter,
		(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id=$1))
		FROM workspace WHERE id=$1`, testWorkspaceID).Scan(&counterBefore)
	worker := seedAllowListedAgent(t, "Inbox delegated worker", testUserID, "public_to", testUserID)
	chain := newCrossIssueChain(t, testUserID, testUserID)
	agentComments(t, chain.CoordinatorID, chain.TaskA, chain.IssueY, mention(worker), "").Want(http.StatusCreated)
	taskID, originator, ok := queuedTaskFor(t, chain.IssueY, worker)
	if !ok || uuidToString(originator) != testUserID {
		t.Fatal("delegation did not preserve the upstream human")
	}
	runTo(t, taskID)
	// Delegation normally has a runtime selected when claimed.
	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_id=$2 WHERE id=$1`, taskID, testRuntimeID)
	token := inboxTaskCredential(t, taskID, worker, testWorkspaceID, testUserID)
	// Allocate delegation issue numbers before introducing the fixture's random high numbers.
	f := newPrivateProjectFixture(t)
	secret := insertSecretInboxItem(t, f, testUserID)
	list := func() map[string]bool {
		var items []InboxItemResponse
		testutil.Call(t, inboxAccessHandler(nil), inboxCredentialRequest(token, testWorkspaceID)).Want(http.StatusOK).JSON(&items)
		ids := map[string]bool{}
		for _, item := range items {
			ids[item.ID] = true
		}
		return ids
	}
	if !list()[secret] {
		t.Fatal("delegated own run cannot see permitted private notification")
	}
	removeProjectMemberRow(t, f.projectID, testUserID)
	if list()[secret] {
		t.Fatal("private notification survived membership revocation")
	}
	if state := loadInboxItemState(t, secret); state != (inboxItemState{}) {
		t.Fatalf("GET mutated hidden notification: %+v", state)
	}
	// Loss of an upstream human must never borrow the accountable owner.
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL,accountable_user_id=$2 WHERE id=$1`, taskID, testUserID)
	testutil.Call(t, inboxAccessHandler(nil), inboxCredentialRequest(token, testWorkspaceID)).Want(http.StatusForbidden)

	var counterAfter int64
	dbfx.QueryRow(t, `SELECT issue_counter FROM workspace WHERE id=$1`, testWorkspaceID).Scan(&counterAfter)
	if counterAfter != counterBefore+2 {
		t.Fatalf("issue counter advanced from %d to %d; want %d for the two delegation issues without entering the private fixture's random range",
			counterBefore, counterAfter, counterBefore+2)
	}
	t.Logf("delegation issue counter: %d -> %d", counterBefore, counterAfter)
}

func TestArchivedInboxOwnerReadIsolation(t *testing.T) {
	other := createPlainMember(t, "inbox-archived-recipient-"+uuid.NewString()+"@example.test")
	foreignWorkspace := dbfx.Workspace(t, "Foreign archived inbox", "inbox-archived-"+uuid.NewString())
	dbfx.Member(t, foreignWorkspace, testUserID, "owner")
	newArchived := func(workspaceID, recipientID string) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{"workspace_id": workspaceID, "archived": true,
			"recipient_type": "member", "recipient_id": recipientID, "type": "system", "severity": "info", "title": "Private archived notice"})
	}
	own := newArchived(testWorkspaceID, testUserID)
	otherItem := newArchived(testWorkspaceID, other)
	foreign := newArchived(foreignWorkspace, testUserID)
	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": testUserID, "name": "Archived inbox test", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12]})
	runtimeID := dbfx.Runtime(t, "Own archived inbox runtime")
	agentID := dbfx.Agent(t, "Own archived inbox agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	taskToken := inboxTaskCredential(t, taskID, agentID, testWorkspaceID, testUserID)
	reads := map[string]func(*testing.T, *http.Request) []InboxItemResponse{
		"archived": func(t *testing.T, req *http.Request) []InboxItemResponse {
			var items []InboxItemResponse
			testutil.Call(t, inboxOwnerReads[1].serve(testHandler, nil), req).Want(http.StatusOK).JSON(&items)
			return items
		},
		"archived page": func(t *testing.T, req *http.Request) []InboxItemResponse {
			var page archivedInboxPageResponse
			testutil.Call(t, inboxOwnerReads[2].serve(testHandler, nil), req).Want(http.StatusOK).JSON(&page)
			return page.Items
		},
	}
	paths := map[string]string{"archived": inboxOwnerReads[1].path, "archived page": inboxOwnerReads[2].path}
	for _, cred := range []struct{ name, token string }{{"personal PAT", pat}, {"self-triggered task", taskToken}} {
		for name, read := range reads {
			t.Run(cred.name+"/"+name, func(t *testing.T) {
				req := testutil.WithHeaders(testutil.JSONRequest(http.MethodGet, paths[name]+"?recipient_id="+other, nil),
					"Authorization", "Bearer "+cred.token, "X-Workspace-ID", testWorkspaceID, "X-User-ID", other)
				found := false
				for _, item := range read(t, req) {
					if item.ID == otherItem || item.ID == foreign {
						t.Fatalf("another recipient or workspace leaked: %s", item.ID)
					}
					found = found || item.ID == own
				}
				if !found {
					t.Fatal("own archived notification missing")
				}
				if state := loadInboxItemState(t, own); state != (inboxItemState{archived: true}) {
					t.Fatalf("GET mutated state: %+v", state)
				}
			})
		}
	}
}

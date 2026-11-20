package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
)

// inboxRoutesWithoutOwnerGate lists the /api/inbox routes that may serve an
// agent the inbox owner gate (requireInboxOwnerAccess) would deny, keyed
// "METHOD pattern", each with the reason it is safe. Every other route must
// answer 403 to both machine credentials in
// TestEveryInboxRouteDeniesMachineCredentialsWithoutOwnerTrigger.
var inboxRoutesWithoutOwnerGate = map[string]string{}

// inboxRouteParams resolves each URL parameter an /api/inbox route declares to
// a row of the owner's fixture, so a missing gate reaches the owner's data
// instead of failing on a lookup.
var inboxRouteParams = map[string]func(inboxOwnerGateFixture) string{
	"id":      func(f inboxOwnerGateFixture) string { return f.itemID },
	"issueId": func(f inboxOwnerGateFixture) string { return f.issueID },
}

var chiRouteParam = regexp.MustCompile(`\{([^}:]+)(:[^}]*)?\}`)

type inboxOwnerGateFixture struct {
	db              *dbfx.Fixture
	issueID, itemID string
}

type inboxRoute struct{ method, pattern string }

func (r inboxRoute) key() string { return r.method + " " + r.pattern }

func walkInboxRoutes(t *testing.T, router chi.Routes) []inboxRoute {
	t.Helper()
	var routes []inboxRoute
	err := chi.Walk(router, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if pattern == "/api/inbox" || strings.HasPrefix(pattern, "/api/inbox/") {
			routes = append(routes, inboxRoute{method, pattern})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("router has no /api/inbox routes; the walk no longer reaches the inbox group")
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].key() < routes[j].key() })
	return routes
}

func (f inboxOwnerGateFixture) path(t *testing.T, pattern string) string {
	t.Helper()
	return chiRouteParam.ReplaceAllStringFunc(pattern, func(param string) string {
		name := chiRouteParam.FindStringSubmatch(param)[1]
		value, ok := inboxRouteParams[name]
		if !ok {
			t.Fatalf("%s declares {%s}; add a fixture value for it to inboxRouteParams", pattern, name)
		}
		return value(f)
	})
}

func (f inboxOwnerGateFixture) requireItemUntouched(t *testing.T) {
	t.Helper()
	if f.db.Count(t, `SELECT count(*) FROM inbox_item WHERE id = $1`, f.itemID) != 1 {
		t.Fatal("denied call deleted the owner's notification")
	}
	var read, archived, snoozed bool
	f.db.QueryRow(t, `SELECT read, archived, snoozed_until IS NOT NULL FROM inbox_item WHERE id = $1`, f.itemID).Scan(&read, &archived, &snoozed)
	if read || archived || snoozed {
		t.Fatalf("denied call changed the owner's notification: read=%v archived=%v snoozed=%v", read, archived, snoozed)
	}
}

func mintInboxTaskToken(t *testing.T, f *dbfx.Fixture, taskID, agentID string) string {
	t.Helper()
	token := "mat_" + uuid.NewString()
	f.InsertNoID(t, "task_token", dbfx.Cols{
		"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID,
		"workspace_id": f.WorkspaceID, "user_id": f.UserID,
		"expires_at": dbfx.Raw("now() + interval '1 hour'"),
	}, "token_hash = $1", auth.HashToken(token))
	return token
}

// Each handler under /api/inbox calls the owner gate itself, so a new route
// that forgets it would let an agent act on its runtime owner's inbox for any
// human. This walks the real router, so a new route is covered the moment it
// is registered: an agent run another human triggered, and a cloud-node
// credential claiming an owner-triggered run, must both be refused.
func TestEveryInboxRouteDeniesMachineCredentialsWithoutOwnerTrigger(t *testing.T) {
	db := dbfx.New(testPool, testWorkspaceID, testUserID)
	db.WorkspaceID = db.Workspace(t, "Inbox owner gate routes", "inbox-gate-routes-"+uuid.NewString())
	db.Member(t, db.WorkspaceID, db.UserID, "owner")
	db.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, db.WorkspaceID)
	other := db.User(t, "Inbox gate other human", "inbox-gate-other-"+uuid.NewString()+"@example.test")
	runtimeID := db.Runtime(t, "Inbox gate runtime")
	agentID := db.Agent(t, "Inbox gate agent", runtimeID)
	runningTask := func(originator string) string {
		return db.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID, "status": "running",
			"originator_user_id": originator, "accountable_user_id": originator})
	}
	f := inboxOwnerGateFixture{db: db, issueID: db.Issue(t, "Inbox gate issue")}
	ownerTaskID := runningTask(db.UserID)
	ownerTriggered := mintInboxTaskToken(t, db, ownerTaskID, agentID)
	otherTriggered := mintInboxTaskToken(t, db, runningTask(other), agentID)

	cloudToken := "mcn_" + uuid.NewString()
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Token != cloudToken {
			http.Error(w, "unexpected verification request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "owner_id": db.UserID})
	}))
	t.Cleanup(fleet.Close)
	t.Setenv("MULTICA_CLOUD_URL", fleet.URL)
	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})

	call := func(method, path string, headers ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-Workspace-ID", db.WorkspaceID)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	credentials := []struct {
		name    string
		headers []string
	}{
		{"agent run another human triggered", []string{"Authorization", "Bearer " + otherTriggered}},
		{"cloud node claiming an owner-triggered run", []string{"Authorization", "Bearer " + cloudToken,
			"X-Actor-Source", "task_token", "X-Task-ID", ownerTaskID, "X-Agent-ID", agentID}},
	}

	// Control: the same agent, workspace and runtime pass the gate when the
	// owner triggered the run, so a 403 below is the gate's answer and not a
	// broken fixture.
	if rec := call(http.MethodGet, "/api/inbox", "Authorization", "Bearer "+ownerTriggered); rec.Code != http.StatusOK {
		t.Fatalf("owner-triggered control: status %d, want 200: %s", rec.Code, rec.Body)
	}

	routes := walkInboxRoutes(t, router)
	walked := map[string]bool{}
	for _, route := range routes {
		walked[route.key()] = true
		if _, exempt := inboxRoutesWithoutOwnerGate[route.key()]; exempt {
			continue
		}
		t.Run(route.key(), func(t *testing.T) {
			f := f
			f.itemID = db.Insert(t, "inbox_item", dbfx.Cols{"workspace_id": db.WorkspaceID, "issue_id": f.issueID,
				"recipient_type": "member", "recipient_id": db.UserID, "type": "new_comment", "severity": "info", "title": "Owner notice"})
			path := f.path(t, route.pattern)
			for _, cred := range credentials {
				if rec := call(route.method, path, cred.headers...); rec.Code != http.StatusForbidden {
					t.Errorf("%s: status %d, want 403; does the handler call requireInboxOwnerAccess? %s", cred.name, rec.Code, rec.Body)
				}
				f.requireItemUntouched(t)
			}
		})
	}
	for key := range inboxRoutesWithoutOwnerGate {
		if !walked[key] {
			t.Errorf("inboxRoutesWithoutOwnerGate lists %q, which the router no longer registers", key)
		}
	}
}

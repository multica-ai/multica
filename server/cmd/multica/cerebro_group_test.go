// CEREBRO-PATCH(cerebro-groups-cli-test): JEH-1172 cerebro-only file — group CLI tests.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	cerebroGroupTestWorkspaceID = "ws-1"
	cerebroGroupTestUserID      = "11111111-1111-1111-1111-111111111111"
	cerebroGroupTestGroupID     = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	cerebroGroupTestRuntimeID   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	cerebroGroupTestAgentID     = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	cerebroGroupTestProjectID   = "dddddddd-dddd-dddd-dddd-dddddddddddd"
)

// cerebroGroupTestServer is a tiny in-memory stand-in for the cerebro server.
// It supports the routes the CLI hits in tests; behavior is intentionally
// minimal — we're verifying the CLI's request shape, not the server.
type cerebroGroupTestServer struct {
	mu       *requestRecorder
	groups   []map[string]any
	members  []map[string]any
	agents   []map[string]any
	runtimes []map[string]any
}

type requestRecorder struct {
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

func newCerebroGroupTestServer() (*httptest.Server, *requestRecorder) {
	rec := &requestRecorder{}
	state := &cerebroGroupTestServer{
		mu: rec,
		groups: []map[string]any{
			{
				"id":          cerebroGroupTestGroupID,
				"name":        "Engineering",
				"description": "engineers",
				"created_at":  "2026-05-01T00:00:00Z",
			},
		},
		members: []map[string]any{
			{
				"user_id": cerebroGroupTestUserID,
				"name":    "Alice Smith",
				"email":   "alice@example.com",
			},
		},
		agents: []map[string]any{
			{
				"id":   cerebroGroupTestAgentID,
				"name": "Codex Agent",
			},
		},
		runtimes: []map[string]any{
			{
				"id":   cerebroGroupTestRuntimeID,
				"name": "Default Runtime",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil && r.Body != http.NoBody {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				_ = json.Unmarshal(data, &body)
			}
		}
		rec.requests = append(rec.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Body:   body,
		})

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/"+cerebroGroupTestWorkspaceID+"/groups":
			_ = json.NewEncoder(w).Encode(state.groups)
		case r.Method == http.MethodPost && r.URL.Path == "/api/workspaces/"+cerebroGroupTestWorkspaceID+"/groups":
			created := map[string]any{
				"id":          cerebroGroupTestGroupID,
				"name":        body["name"],
				"description": body["description"],
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/groups/"+cerebroGroupTestGroupID):
			suffix := strings.TrimPrefix(r.URL.Path, "/api/groups/"+cerebroGroupTestGroupID)
			switch suffix {
			case "":
				_ = json.NewEncoder(w).Encode(state.groups[0])
			case "/members":
				_ = json.NewEncoder(w).Encode([]map[string]any{{
					"group_id":   cerebroGroupTestGroupID,
					"user_id":    cerebroGroupTestUserID,
					"user_name":  "Alice Smith",
					"user_email": "alice@example.com",
					"added_at":   "2026-05-01T00:00:00Z",
				}})
			case "/capabilities":
				_ = json.NewEncoder(w).Encode([]map[string]any{{
					"group_id":   cerebroGroupTestGroupID,
					"capability": "create_runtime",
				}})
			case "/runtimes":
				_ = json.NewEncoder(w).Encode([]map[string]any{{
					"group_id":   cerebroGroupTestGroupID,
					"runtime_id": cerebroGroupTestRuntimeID,
				}})
			case "/agents":
				_ = json.NewEncoder(w).Encode([]map[string]any{{
					"group_id": cerebroGroupTestGroupID,
					"agent_id": cerebroGroupTestAgentID,
				}})
			default:
				http.NotFound(w, r)
			}
		case r.Method == http.MethodPatch && r.URL.Path == "/api/groups/"+cerebroGroupTestGroupID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          cerebroGroupTestGroupID,
				"name":        coalesce(body["name"], state.groups[0]["name"]),
				"description": coalesce(body["description"], state.groups[0]["description"]),
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/groups/"+cerebroGroupTestGroupID):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/"+cerebroGroupTestGroupID+"/members":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"group_id":   cerebroGroupTestGroupID,
				"user_id":    body["user_id"],
				"user_name":  "Alice Smith",
				"user_email": "alice@example.com",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/"+cerebroGroupTestGroupID+"/capabilities":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"group_id":   cerebroGroupTestGroupID,
				"capability": body["capability"],
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/"+cerebroGroupTestGroupID+"/runtimes":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"group_id":   cerebroGroupTestGroupID,
				"runtime_id": body["runtime_id"],
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/"+cerebroGroupTestGroupID+"/agents":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"group_id": cerebroGroupTestGroupID,
				"agent_id": body["agent_id"],
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/"+cerebroGroupTestWorkspaceID+"/members":
			_ = json.NewEncoder(w).Encode(state.members)
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode(state.agents)
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/"+cerebroGroupTestProjectID+"/group-access":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"project_id": cerebroGroupTestProjectID,
				"group_id":   cerebroGroupTestGroupID,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/"+cerebroGroupTestProjectID+"/group-access":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project_id": cerebroGroupTestProjectID,
				"group_id":   body["group_id"],
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/projects/"+cerebroGroupTestProjectID+"/group-access/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"id":     cerebroGroupTestProjectID,
					"title":  "Demo Project",
					"status": "in_progress",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, rec
}

func coalesce(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func newCerebroGroupTestClient(srv *httptest.Server) *cli.APIClient {
	return cli.NewAPIClient(srv.URL, cerebroGroupTestWorkspaceID, "test-token")
}

func findRequest(rec *requestRecorder, method, path string) (recordedRequest, bool) {
	for _, r := range rec.requests {
		if r.Method == method && r.Path == path {
			return r, true
		}
	}
	return recordedRequest{}, false
}

// TestResolveGroupID covers UUID passthrough, exact name match, and the
// substring fallback that lets `multica group ... <name>` work without UUIDs.
func TestResolveGroupID(t *testing.T) {
	srv, _ := newCerebroGroupTestServer()
	defer srv.Close()
	client := newCerebroGroupTestClient(srv)

	t.Run("passes through a UUID without lookup", func(t *testing.T) {
		got, err := resolveGroupID(context.Background(), client, cerebroGroupTestGroupID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != cerebroGroupTestGroupID {
			t.Errorf("got %q, want %q", got.ID, cerebroGroupTestGroupID)
		}
	})

	t.Run("matches by exact name", func(t *testing.T) {
		got, err := resolveGroupID(context.Background(), client, "Engineering")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != cerebroGroupTestGroupID {
			t.Errorf("got %q, want %q", got.ID, cerebroGroupTestGroupID)
		}
	})

	t.Run("matches case-insensitive substring", func(t *testing.T) {
		got, err := resolveGroupID(context.Background(), client, "engin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != cerebroGroupTestGroupID {
			t.Errorf("got %q, want %q", got.ID, cerebroGroupTestGroupID)
		}
	})

	t.Run("missing input returns error", func(t *testing.T) {
		if _, err := resolveGroupID(context.Background(), client, ""); err == nil {
			t.Fatal("expected error for empty input")
		}
	})

	t.Run("no workspace returns helpful error", func(t *testing.T) {
		noWSClient := cli.NewAPIClient(srv.URL, "", "test-token")
		_, err := resolveGroupID(context.Background(), noWSClient, "Engineering")
		if err == nil || !strings.Contains(err.Error(), "workspace") {
			t.Fatalf("expected workspace error, got: %v", err)
		}
	})
}

// TestResolveWorkspaceMemberID verifies the user resolver used by member
// add/remove. Name lookups must hit the workspace members endpoint exactly
// once; UUIDs short-circuit the API entirely.
func TestResolveWorkspaceMemberID(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()
	client := newCerebroGroupTestClient(srv)

	t.Run("returns canonical UUID for canonical UUID input", func(t *testing.T) {
		got, err := resolveWorkspaceMemberID(context.Background(), client, cerebroGroupTestUserID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cerebroGroupTestUserID {
			t.Errorf("got %q, want %q", got, cerebroGroupTestUserID)
		}
	})

	t.Run("resolves a name to a UUID via /members", func(t *testing.T) {
		rec.requests = nil
		got, err := resolveWorkspaceMemberID(context.Background(), client, "Alice")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cerebroGroupTestUserID {
			t.Errorf("got %q, want %q", got, cerebroGroupTestUserID)
		}
		if _, ok := findRequest(rec, http.MethodGet, "/api/workspaces/"+cerebroGroupTestWorkspaceID+"/members"); !ok {
			t.Fatal("expected /members lookup")
		}
	})

	t.Run("resolves by email", func(t *testing.T) {
		got, err := resolveWorkspaceMemberID(context.Background(), client, "alice@example.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cerebroGroupTestUserID {
			t.Errorf("got %q, want %q", got, cerebroGroupTestUserID)
		}
	})

	t.Run("returns error for unknown member", func(t *testing.T) {
		if _, err := resolveWorkspaceMemberID(context.Background(), client, "no-such-user"); err == nil {
			t.Fatal("expected not-found error")
		}
	})
}

// TestGroupCreate is the canonical CLI happy-path test: the cobra command is
// invoked with --name and --description, and we assert (1) it succeeded and
// (2) the recorded HTTP request hit the workspace-scoped create endpoint
// with the same body.
func TestGroupCreate(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"name", "description"} {
			_ = groupCreateCmd.Flags().Set(n, "")
			if f := groupCreateCmd.Flags().Lookup(n); f != nil {
				f.Changed = false
			}
		}
	})

	if err := groupCreateCmd.Flags().Set("name", "Data"); err != nil {
		t.Fatal(err)
	}
	if err := groupCreateCmd.Flags().Set("description", "data engineering"); err != nil {
		t.Fatal(err)
	}
	if err := groupCreateCmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}

	withCLIEnv(t, srv.URL, cerebroGroupTestWorkspaceID, func() {
		if err := runGroupCreate(groupCreateCmd, nil); err != nil {
			t.Fatalf("runGroupCreate: %v", err)
		}
	})

	req, ok := findRequest(rec, http.MethodPost, "/api/workspaces/"+cerebroGroupTestWorkspaceID+"/groups")
	if !ok {
		t.Fatal("expected POST to /api/workspaces/{id}/groups")
	}
	if req.Body["name"] != "Data" {
		t.Errorf("name = %v, want Data", req.Body["name"])
	}
	if req.Body["description"] != "data engineering" {
		t.Errorf("description = %v, want 'data engineering'", req.Body["description"])
	}
}

// TestGroupCreateRequiresName mirrors the project CLI's "--title is required"
// contract — empty / unset --name must error before any HTTP call.
func TestGroupCreateRequiresName(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		for _, n := range []string{"name", "description"} {
			_ = groupCreateCmd.Flags().Set(n, "")
			if f := groupCreateCmd.Flags().Lookup(n); f != nil {
				f.Changed = false
			}
		}
	})

	withCLIEnv(t, srv.URL, cerebroGroupTestWorkspaceID, func() {
		err := runGroupCreate(groupCreateCmd, nil)
		if err == nil || !strings.Contains(err.Error(), "--name is required") {
			t.Fatalf("expected --name required error, got: %v", err)
		}
	})

	if len(rec.requests) != 0 {
		t.Errorf("expected zero HTTP requests, got %d", len(rec.requests))
	}
}

// TestGroupCapabilitySet exercises the grouppermissions API path — the
// admin-only routes are gated server-side; here we just verify the CLI hits
// the right endpoint with the right body.
func TestGroupCapabilitySet(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		_ = groupCapabilitySetCmd.Flags().Set("output", "json")
	})

	withCLIEnv(t, srv.URL, cerebroGroupTestWorkspaceID, func() {
		if err := runGroupCapabilitySet(groupCapabilitySetCmd, []string{cerebroGroupTestGroupID, "create_runtime"}); err != nil {
			t.Fatalf("runGroupCapabilitySet: %v", err)
		}
	})

	req, ok := findRequest(rec, http.MethodPost, "/api/groups/"+cerebroGroupTestGroupID+"/capabilities")
	if !ok {
		t.Fatal("expected POST to /api/groups/{id}/capabilities")
	}
	if req.Body["capability"] != "create_runtime" {
		t.Errorf("capability = %v, want create_runtime", req.Body["capability"])
	}
}

// TestGroupRuntimeAddRequiresUUID guards the runtime-id input — passing a
// non-UUID must short-circuit, since the server enforces the same rule and
// would return 400 anyway.
func TestGroupRuntimeAddRequiresUUID(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()

	withCLIEnv(t, srv.URL, cerebroGroupTestWorkspaceID, func() {
		err := runGroupRuntimeAdd(groupRuntimeAddCmd, []string{cerebroGroupTestGroupID, "not-a-uuid"})
		if err == nil || !strings.Contains(err.Error(), "UUID") {
			t.Fatalf("expected UUID error, got: %v", err)
		}
	})

	if len(rec.requests) != 0 {
		t.Errorf("expected zero HTTP requests, got %d", len(rec.requests))
	}
}

// TestGroupProjectAdd verifies the project group-access path: the CLI must
// resolve both the project and the group, then POST to /group-access.
func TestGroupProjectAdd(t *testing.T) {
	srv, rec := newCerebroGroupTestServer()
	defer srv.Close()

	t.Cleanup(func() {
		_ = groupProjectAddCmd.Flags().Set("output", "json")
	})

	withCLIEnv(t, srv.URL, cerebroGroupTestWorkspaceID, func() {
		if err := runGroupProjectAdd(groupProjectAddCmd, []string{cerebroGroupTestProjectID, cerebroGroupTestGroupID}); err != nil {
			t.Fatalf("runGroupProjectAdd: %v", err)
		}
	})

	req, ok := findRequest(rec, http.MethodPost, "/api/projects/"+cerebroGroupTestProjectID+"/group-access")
	if !ok {
		t.Fatal("expected POST to /api/projects/{id}/group-access")
	}
	if req.Body["group_id"] != cerebroGroupTestGroupID {
		t.Errorf("group_id = %v, want %s", req.Body["group_id"], cerebroGroupTestGroupID)
	}
}

// withCLIEnv sets the env vars newAPIClient reads from and restores them
// when the test function returns. Tests must run sequentially (we mutate
// process env) but each test that uses this helper is isolated.
func withCLIEnv(t *testing.T, serverURL, workspaceID string, fn func()) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", workspaceID)
	t.Setenv("MULTICA_TOKEN", "test-token")
	fn()
}

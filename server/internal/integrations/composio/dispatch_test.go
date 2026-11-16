package composio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedActiveConnection writes a single active row for the user/toolkit pair
// so BuildTaskOverlay's "user has at least one active connection" branch is
// reachable in tests without touching the real Composio API or DB.
func seedActiveConnection(t *testing.T, store *fakeStore, userID pgtype.UUID, toolkit, connectedAccountID string) {
	t.Helper()
	if _, err := store.UpsertUserComposioConnection(context.Background(), db.UpsertUserComposioConnectionParams{
		UserID:             userID,
		ToolkitSlug:        toolkit,
		AuthConfigID:       "ac_test",
		ConnectedAccountID: connectedAccountID,
		ComposioUserID:     uuidToString(userID),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	idx := 0
	for i, x := range b {
		out[idx] = hex[x>>4]
		out[idx+1] = hex[x&0xf]
		idx += 2
		if i == 3 || i == 5 || i == 7 || i == 9 {
			out[idx] = '-'
			idx++
		}
	}
	return string(out)
}

// makeAgent returns an Agent fixture with the given owner and optional
// allowlist. Other Agent fields are zero-valued because BuildTaskOverlay
// only reads OwnerID + ComposioToolkitAllowlist.
func makeAgent(owner pgtype.UUID, allowlist ...string) db.Agent {
	a := db.Agent{OwnerID: owner}
	if allowlist != nil {
		a.ComposioToolkitAllowlist = allowlist
	}
	return a
}

// --- Overlay follows the agent owner, not the run originator (MUL-3963) ---

// TestBuildTaskOverlay_FollowsOwnerRegardlessOfOriginator: with no human
// originator (autopilot / system run) the overlay is STILL built from the
// agent owner's connected apps, because the invocation-permission gate that
// decides who may run the agent lives upstream (canInvokeAgent /
// canCreatorInvokeAgent). BuildTaskOverlay no longer gates on the originator.
func TestBuildTaskOverlay_FollowsOwnerRegardlessOfOriginator(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/noorig"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)

	owner := mintUUID(7)
	agent := makeAgent(owner, "notion")
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

	result, err := svc.BuildTaskOverlay(context.Background(), pgtype.UUID{}, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatalf("expected overlay from owner connection even with no originator")
	}
	if sdkFake.lastSessReq.UserID != uuidToString(owner) {
		t.Errorf("CreateSession user id = %q, want owner %q", sdkFake.lastSessReq.UserID, uuidToString(owner))
	}
}

// --- Overlay uses the OWNER's connection, not the originator's -----------

// TestBuildTaskOverlay_UsesOwnerConnectionNotOriginator is the MUL-3963
// contract that replaced the old originator==owner gate: a non-owner
// originator who has passed the invoke gate gets the overlay built from the
// AGENT OWNER's connection, and never from their own. Seeding only the
// non-owner originator's connection (owner has none) produces no overlay.
func TestBuildTaskOverlay_UsesOwnerConnectionNotOriginator(t *testing.T) {
	t.Parallel()

	owner := mintUUID(11)
	other := mintUUID(12) // a different human who triggered the run

	// Case A: owner HAS the connection -> overlay built with OWNER identity,
	// even though the originator is a different user.
	t.Run("owner-connection-used", func(t *testing.T) {
		t.Parallel()
		sdkFake := &fakeSDK{
			createSessResp: &sdk.CreateSessionResponse{
				MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/owner"},
			},
		}
		store := newFakeStore()
		svc := newTestService(t, sdkFake, store)
		agent := makeAgent(owner, "notion")
		seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")
		// The originator also has a matching connection; it must NOT be used.
		seedActiveConnection(t, store, other, "notion", "ca_other_notion")

		result, err := svc.BuildTaskOverlay(context.Background(), other, agent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.MCPOverlay) == 0 {
			t.Fatalf("expected overlay built from owner connection")
		}
		if sdkFake.lastSessReq.UserID != uuidToString(owner) {
			t.Errorf("CreateSession user id = %q, want owner %q (not originator)", sdkFake.lastSessReq.UserID, uuidToString(owner))
		}
		assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_owner_notion")
	})

	// Case B: only the originator has the connection (owner has none) -> the
	// intersection with the OWNER's connections is empty, so no overlay.
	t.Run("originator-connection-ignored", func(t *testing.T) {
		t.Parallel()
		sdkFake := &fakeSDK{}
		store := newFakeStore()
		svc := newTestService(t, sdkFake, store)
		agent := makeAgent(owner, "notion")
		seedActiveConnection(t, store, other, "notion", "ca_other_notion")

		result, err := svc.BuildTaskOverlay(context.Background(), other, agent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.MCPOverlay != nil {
			t.Errorf("expected nil overlay: owner has no connection, originator's must be ignored, got %s", string(result.MCPOverlay))
		}
		if sdkFake.createSessCalls != 0 {
			t.Errorf("CreateSession must not run when owner has no matching connection, got %d", sdkFake.createSessCalls)
		}
	})
}

// --- Gate 3: empty / NULL allowlist --------------------------------------

// TestBuildTaskOverlay_EmptyAllowlistIsNoOp covers both NULL and `{}`
// columns: until the agent owner has opted into specific toolkits, the
// dispatch decision is OFF — no overlay, no Composio call, no token.
func TestBuildTaskOverlay_EmptyAllowlistIsNoOp(t *testing.T) {
	t.Parallel()
	for name, allowlist := range map[string][]string{
		"nil-slice":   nil,
		"empty-slice": {},
		"whitespace":  {"   ", "\t"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sdkFake := &fakeSDK{}
			store := newFakeStore()
			svc := newTestService(t, sdkFake, store)
			owner := mintUUID(20)
			agent := makeAgent(owner, allowlist...)
			seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

			result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.MCPOverlay != nil {
				t.Errorf("expected nil overlay for empty allowlist, got %s", string(result.MCPOverlay))
			}
			if sdkFake.createSessCalls != 0 {
				t.Errorf("CreateSession must not run when allowlist is empty, got %d calls", sdkFake.createSessCalls)
			}
		})
	}
}

// --- Gate 4: allowlist non-empty but no matching active connection -------

// TestBuildTaskOverlay_NoMatchingConnectionIsNoOp — the owner allowlisted
// toolkits they have not connected (or revoked the connection for). The
// intersection is empty, so we have nothing to mount and must not pay for
// an empty Composio session.
func TestBuildTaskOverlay_NoMatchingConnectionIsNoOp(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(30)
	agent := makeAgent(owner, "notion", "github")
	// Owner connected SLACK only — not in allowlist.
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay for empty intersection, got %s", string(result.MCPOverlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession must not run when intersection is empty, got %d calls", sdkFake.createSessCalls)
	}
}

// --- Happy path: allowlist ∩ active connections is non-empty -------------

// TestBuildTaskOverlay_HappyPath_FiltersBothWays — the canonical
// successful dispatch. Asserts:
//   - CreateSession was called with the Multica user id verbatim
//   - both filters were passed (toolkits.enable AND connected_accounts)
//   - the slug set is exactly the intersection (allowlist ∩ active)
//   - connected_accounts pins the correct connected_account_id per slug
//   - the returned overlay JSON has the daemon-expected shape
//   - connected app metadata is exactly the same intersection for prompt use
//   - non-allowlisted active connections (slack here) do NOT leak through
func TestBuildTaskOverlay_HappyPath_FiltersBothWays(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/abc"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(13)
	agent := makeAgent(owner, "notion", "github")
	// Three active connections; only the two in the allowlist should be
	// surfaced. The third (slack) is the proof that the filter is being
	// applied — without it, every active connection would leak into the
	// session even when the owner did not allowlist it.
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatalf("expected non-empty overlay, got nil")
	}

	// composio_user_id == Multica user id invariant
	if sdkFake.lastSessReq.UserID != uuidToString(owner) {
		t.Errorf("CreateSession user id: got %q, want %q", sdkFake.lastSessReq.UserID, uuidToString(owner))
	}
	// Toolkits.enable filter must be the intersection, not the agent's
	// full allowlist nor the user's full connection set.
	tk, _ := sdkFake.lastSessReq.Toolkits["enable"].([]string)
	if len(tk) != 2 || !containsString(tk, "notion") || !containsString(tk, "github") {
		t.Errorf("CreateSession toolkits.enable = %v, want exactly [notion github]", tk)
	}
	if containsString(tk, "slack") {
		t.Errorf("non-allowlisted slack leaked into toolkits.enable: %v", tk)
	}
	// connected_accounts pinning
	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_owner_notion")
	assertPinnedAccount(t, sdkFake.lastSessReq, "github", "ca_owner_github")
	if _, leaked := sdkFake.lastSessReq.ConnectedAccounts["slack"]; leaked {
		t.Errorf("non-allowlisted slack leaked into connected_accounts")
	}
	assertConnectedApps(t, result.ConnectedApps, "github", "notion")

	var payload mcpOverlayPayload
	if err := json.Unmarshal(result.MCPOverlay, &payload); err != nil {
		t.Fatalf("unmarshal overlay: %v", err)
	}
	srv, ok := payload.MCPServers[mcpOverlayServerName]
	if !ok {
		t.Fatalf("overlay missing %q server, got %s", mcpOverlayServerName, string(result.MCPOverlay))
	}
	if srv.Type != "http" {
		t.Errorf("type: got %q, want \"http\"", srv.Type)
	}
	if srv.URL != "https://mcp.composio.dev/session/abc" {
		t.Errorf("url: got %q", srv.URL)
	}
	if srv.Headers["x-api-key"] != "secret" {
		t.Errorf("headers missing x-api-key: %v", srv.Headers)
	}
}

func TestBuildTaskOverlay_CreateSessionWireContract(t *testing.T) {
	t.Parallel()

	postedCh := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tool_router/session" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key header = %q", got)
		}
		var posted map[string]any
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		postedCh <- posted
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"session_id": "trs_wire",
			"mcp":        map[string]any{"type": "http", "url": "https://mcp.example/session/wire"},
		}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer upstream.Close()

	client, err := sdk.NewClient(sdk.Options{APIKey: "test-key", BaseURL: upstream.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	store := newFakeStore()
	svc := newTestService(t, client, store)
	owner := mintUUID(17)
	agent := makeAgent(owner, "notion", "github")
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("BuildTaskOverlay: %v", err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatal("expected non-empty overlay")
	}
	var posted map[string]any
	select {
	case posted = <-postedCh:
	default:
	}
	if posted == nil {
		t.Fatal("upstream did not receive a request body")
	}
	if got := posted["user_id"]; got != uuidToString(owner) {
		t.Fatalf("user_id = %v, want %q", got, uuidToString(owner))
	}

	toolkits, ok := posted["toolkits"].(map[string]any)
	if !ok {
		t.Fatalf("toolkits = %T(%v), want object", posted["toolkits"], posted["toolkits"])
	}
	assertJSONStringArraySet(t, toolkits["enable"], "notion", "github")
	if _, wrong := toolkits["enabled"]; wrong {
		t.Fatalf("toolkits used unexpected key \"enabled\": %v", toolkits)
	}

	connected, ok := posted["connected_accounts"].(map[string]any)
	if !ok {
		t.Fatalf("connected_accounts = %T(%v), want object", posted["connected_accounts"], posted["connected_accounts"])
	}
	assertJSONStringArraySet(t, connected["notion"], "ca_owner_notion")
	assertJSONStringArraySet(t, connected["github"], "ca_owner_github")
	if _, leaked := connected["slack"]; leaked {
		t.Fatalf("non-allowlisted slack leaked into connected_accounts: %v", connected)
	}
}

// --- Gate 5: defensive empty-URL response --------------------------------

// TestBuildTaskOverlay_EmptyURL guards a defensive branch: Composio
// returning a 200 with an empty mcp.url must not produce a half-baked
// overlay — every runtime sidecar generator would emit a server with an
// empty URL, breaking the task.
func TestBuildTaskOverlay_EmptyURL(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: ""},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(14)
	agent := makeAgent(owner, "github")
	seedActiveConnection(t, store, owner, "github", "ca_owner_github")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay when MCP URL is empty, got %s", string(result.MCPOverlay))
	}
}

// --- SDK error surfacing -------------------------------------------------

// TestBuildTaskOverlay_SDKError — an SDK failure (Composio outage, network
// blip, …) must surface as an error so the caller can log it. The caller
// (TaskService.buildRuntimeMCPOverlay) is responsible for swallowing the error
// and proceeding with no overlay — best-effort enqueue.
func TestBuildTaskOverlay_SDKError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{createSessErr: errors.New("composio: 503 backend")}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(15)
	agent := makeAgent(owner, "slack")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err == nil {
		t.Fatalf("expected error from SDK failure, got nil")
	}
	if !strings.Contains(err.Error(), "create session") {
		t.Errorf("error should mention create session, got %v", err)
	}
	if result.MCPOverlay != nil {
		t.Errorf("expected nil overlay on SDK error, got %s", string(result.MCPOverlay))
	}
}

// --- Slug normalisation regression --------------------------------------

// TestBuildTaskOverlay_NormalisesAllowlistAndConnectionSlugs — a
// defensively normalised compare. The API write path lowers + trims slugs
// before persisting, but DB migrations or out-of-band writes can put
// uppercase / padded entries in the column. The dispatch path must still
// match against (lowercased, trimmed) Composio connection rows so a
// well-intentioned UI typo cannot silently disable the overlay.
func TestBuildTaskOverlay_NormalisesAllowlistAndConnectionSlugs(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/x"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(40)
	// allowlist has whitespace-padded MIXED-case entries.
	agent := makeAgent(owner, " Notion ", "GITHUB")
	// connection rows arrive with the canonical lowercased slugs (which
	// is what the connect flow always writes).
	seedActiveConnection(t, store, owner, "notion", "ca_a")

	result, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MCPOverlay == nil {
		t.Fatalf("expected non-empty overlay despite uppercase/padded allowlist")
	}
	assertPinnedAccount(t, sdkFake.lastSessReq, "notion", "ca_a")
}

// containsString reports whether haystack contains needle. Small local
// helper so the tests don't pull in slices.Contains and stay copy-paste-
// compatible with the existing tests in this package.
func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func assertJSONStringArraySet(t *testing.T, raw any, want ...string) {
	t.Helper()
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("value = %T(%v), want JSON string array %v", raw, raw, want)
	}
	got := make(map[string]struct{}, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("array item = %T(%v), want string", item, item)
		}
		got[s] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("array = %v, want set %v", arr, want)
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Fatalf("array = %v, missing %q", arr, w)
		}
	}
}

func assertPinnedAccount(t *testing.T, req sdk.CreateSessionRequest, slug, want string) {
	t.Helper()
	got, ok := req.ConnectedAccounts[slug].([]string)
	if !ok {
		t.Fatalf("connected_accounts[%s] = %T(%v), want []string{%q}", slug, req.ConnectedAccounts[slug], req.ConnectedAccounts[slug], want)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("connected_accounts[%s] = %v, want [%s]", slug, got, want)
	}
}

func assertConnectedApps(t *testing.T, apps []runtimeapps.ConnectedApp, want ...string) {
	t.Helper()
	if len(apps) != len(want) {
		t.Fatalf("connected apps = %+v, want slugs %v", apps, want)
	}
	got := make(map[string]runtimeapps.ConnectedApp, len(apps))
	for _, app := range apps {
		got[app.ToolkitSlug] = app
	}
	for _, slug := range want {
		app, ok := got[slug]
		if !ok {
			t.Fatalf("connected apps = %+v, missing %q", apps, slug)
		}
		if app.Provider != "composio" {
			t.Errorf("%s provider = %q, want composio", slug, app.Provider)
		}
		if app.ServerName != mcpOverlayServerName {
			t.Errorf("%s server = %q, want %q", slug, app.ServerName, mcpOverlayServerName)
		}
		if app.ToolkitName == "" {
			t.Errorf("%s has empty display name", slug)
		}
	}
	if _, leaked := got["slack"]; leaked {
		t.Fatalf("non-allowlisted slack leaked into connected apps: %+v", apps)
	}
}

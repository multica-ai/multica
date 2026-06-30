package composio

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
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

// --- Gate 1: invalid originator ------------------------------------------

// TestBuildTaskOverlay_NoOriginatorIsNoOp covers the autopilot / system-run
// branch: when there is no human at the top of the chain, we must not
// project anyone's connected apps into the run. The builder is short-
// circuited BEFORE touching the store so a guaranteed-empty result never
// costs a DB query.
func TestBuildTaskOverlay_NoOriginatorIsNoOp(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)

	owner := mintUUID(7)
	agent := makeAgent(owner, "notion")
	seedActiveConnection(t, store, owner, "notion", "ca_owner_notion")

	overlay, err := svc.BuildTaskOverlay(context.Background(), pgtype.UUID{}, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay for invalid originator, got %s", string(overlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("expected zero CreateSession calls, got %d", sdkFake.createSessCalls)
	}
}

// --- Gate 2: originator != agent.OwnerID ---------------------------------

// TestBuildTaskOverlay_OriginatorNotOwnerIsNoOp is the Stage 3.1 contract:
// even when the originator HAS active connections that overlap the agent's
// allowlist, projecting them into the run would let any member who can
// @-mention the agent read into the agent owner's accounts. Must return
// (nil, nil) without calling CreateSession.
func TestBuildTaskOverlay_OriginatorNotOwnerIsNoOp(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)

	owner := mintUUID(11)
	other := mintUUID(12) // a different human in the workspace
	agent := makeAgent(owner, "notion")
	// Even seeding the OTHER user with a matching connection must not
	// produce an overlay — the gate is on identity, not "has any
	// connection to that toolkit".
	seedActiveConnection(t, store, other, "notion", "ca_other_notion")

	overlay, err := svc.BuildTaskOverlay(context.Background(), other, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay when originator != owner, got %s", string(overlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession must not be called for non-owner originator, got %d calls", sdkFake.createSessCalls)
	}
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

			overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if overlay != nil {
				t.Errorf("expected nil overlay for empty allowlist, got %s", string(overlay))
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

	overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay for empty intersection, got %s", string(overlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("CreateSession must not run when intersection is empty, got %d calls", sdkFake.createSessCalls)
	}
}

// --- Happy path: allowlist ∩ active connections is non-empty -------------

// TestBuildTaskOverlay_HappyPath_FiltersBothWays — the canonical
// successful dispatch. Asserts:
//   - CreateSession was called with the Multica user id verbatim
//   - both filters were passed (toolkits.slugs AND connected_accounts)
//   - the slug set is exactly the intersection (allowlist ∩ active)
//   - connected_accounts pins the correct connected_account_id per slug
//   - the returned overlay JSON has the daemon-expected shape
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

	overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(overlay) == 0 {
		t.Fatalf("expected non-empty overlay, got nil")
	}

	// composio_user_id == Multica user id invariant
	if sdkFake.lastSessReq.UserID != uuidToString(owner) {
		t.Errorf("CreateSession user id: got %q, want %q", sdkFake.lastSessReq.UserID, uuidToString(owner))
	}
	// Toolkits.slugs filter must be the intersection, not the agent's
	// full allowlist nor the user's full connection set.
	tk, _ := sdkFake.lastSessReq.Toolkits["slugs"].([]string)
	if len(tk) != 2 || !containsString(tk, "notion") || !containsString(tk, "github") {
		t.Errorf("CreateSession toolkits.slugs = %v, want exactly [notion github]", tk)
	}
	if containsString(tk, "slack") {
		t.Errorf("non-allowlisted slack leaked into toolkits.slugs: %v", tk)
	}
	// connected_accounts pinning
	if got := sdkFake.lastSessReq.ConnectedAccounts["notion"]; got != "ca_owner_notion" {
		t.Errorf("connected_accounts[notion] = %v, want ca_owner_notion", got)
	}
	if got := sdkFake.lastSessReq.ConnectedAccounts["github"]; got != "ca_owner_github" {
		t.Errorf("connected_accounts[github] = %v, want ca_owner_github", got)
	}
	if _, leaked := sdkFake.lastSessReq.ConnectedAccounts["slack"]; leaked {
		t.Errorf("non-allowlisted slack leaked into connected_accounts")
	}

	var payload mcpOverlayPayload
	if err := json.Unmarshal(overlay, &payload); err != nil {
		t.Fatalf("unmarshal overlay: %v", err)
	}
	srv, ok := payload.MCPServers[mcpOverlayServerName]
	if !ok {
		t.Fatalf("overlay missing %q server, got %s", mcpOverlayServerName, string(overlay))
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

	overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay when MCP URL is empty, got %s", string(overlay))
	}
}

// --- SDK error surfacing -------------------------------------------------

// TestBuildTaskOverlay_SDKError — an SDK failure (Composio outage, network
// blip, …) must surface as an error so the caller can log it. The caller
// (TaskService.applyRuntimeMCPOverlay) is responsible for swallowing the
// error and proceeding with no overlay — best-effort enqueue.
func TestBuildTaskOverlay_SDKError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{createSessErr: errors.New("composio: 503 backend")}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	owner := mintUUID(15)
	agent := makeAgent(owner, "slack")
	seedActiveConnection(t, store, owner, "slack", "ca_owner_slack")

	overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err == nil {
		t.Fatalf("expected error from SDK failure, got nil")
	}
	if !strings.Contains(err.Error(), "create session") {
		t.Errorf("error should mention create session, got %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay on SDK error, got %s", string(overlay))
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

	overlay, err := svc.BuildTaskOverlay(context.Background(), owner, agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay == nil {
		t.Fatalf("expected non-empty overlay despite uppercase/padded allowlist")
	}
	if got := sdkFake.lastSessReq.ConnectedAccounts["notion"]; got != "ca_a" {
		t.Errorf("normalised match failed: connected_accounts[notion] = %v", got)
	}
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

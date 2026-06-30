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

// TestBuildTaskOverlay_NoConnections is the load-bearing zero-cost branch:
// a user with no active rows must NOT cause CreateMCPSession to fire, and
// must NOT emit any overlay JSON. This is the property that lets Composio
// scale with the active-connect population, not the total task population.
func TestBuildTaskOverlay_NoConnections(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)

	user := mintUUID(7)
	overlay, err := svc.BuildTaskOverlay(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay for user with no connections, got %s", string(overlay))
	}
	if sdkFake.createSessCalls != 0 {
		t.Errorf("expected zero CreateSession calls for empty user, got %d", sdkFake.createSessCalls)
	}
}

// TestBuildTaskOverlay_WithConnections — the happy path. The overlay must
// be the exact shape the daemon-side merge expects:
//
//	{"mcpServers": {"composio": {"type": "http", "url": "...", "headers": {...}}}}
//
// We also assert that CreateSession was sent the Multica user id verbatim
// (the composio_user_id == Multica user id invariant the rest of the
// integration depends on).
func TestBuildTaskOverlay_WithConnections(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: "https://mcp.composio.dev/session/abc"},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	user := mintUUID(13)
	seedActiveConnection(t, store, user, "notion", "ca_user_notion")

	overlay, err := svc.BuildTaskOverlay(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(overlay) == 0 {
		t.Fatalf("expected non-empty overlay, got nil")
	}
	// MCP session was provisioned with the Multica user id verbatim.
	if sdkFake.lastSessReq.UserID != uuidToString(user) {
		t.Errorf("CreateSession user id: got %q, want %q", sdkFake.lastSessReq.UserID, uuidToString(user))
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

// TestBuildTaskOverlay_EmptyURL guards a defensive branch: Composio
// returning a 200 with an empty mcp.url must not produce a half-baked
// overlay (every runtime sidecar generator would emit a server with an
// empty URL, breaking the task).
func TestBuildTaskOverlay_EmptyURL(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{
		createSessResp: &sdk.CreateSessionResponse{
			MCP: sdk.MCPDescriptor{URL: ""},
		},
	}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	user := mintUUID(14)
	seedActiveConnection(t, store, user, "github", "ca_user_github")

	overlay, err := svc.BuildTaskOverlay(context.Background(), user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if overlay != nil {
		t.Errorf("expected nil overlay when MCP URL is empty, got %s", string(overlay))
	}
}

// TestBuildTaskOverlay_SDKError — an SDK failure (Composio outage, network
// blip, …) must surface as an error so the caller can log it. The caller
// (TaskService.applyRuntimeMCPOverlay) is responsible for swallowing the
// error and proceeding with no overlay — best-effort enqueue.
func TestBuildTaskOverlay_SDKError(t *testing.T) {
	t.Parallel()
	sdkFake := &fakeSDK{createSessErr: errors.New("composio: 503 backend")}
	store := newFakeStore()
	svc := newTestService(t, sdkFake, store)
	user := mintUUID(15)
	seedActiveConnection(t, store, user, "slack", "ca_user_slack")

	overlay, err := svc.BuildTaskOverlay(context.Background(), user)
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

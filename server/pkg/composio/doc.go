// Package composio is a small, standalone Go SDK for the Composio v3.1 REST API.
//
// It is intentionally self-contained: the only third-party dependency is
// [github.com/go-resty/resty/v2]. It does not import any Multica-specific
// package, so it can be reused by other Go services or extracted into its
// own module unchanged.
//
// # MVP surface
//
// The SDK targets the surface required by the Composio integration MVP
// (see MUL-3715 / MUL-3720). It is deliberately minimal — only the
// endpoints actually used by the first-stage product are wired up:
//
//   - Connect Link  — POST /connected_accounts/link
//   - MCP Session   — POST /tool_router/session
//   - Connected Accounts — GET /connected_accounts,
//     POST /connected_accounts/{id}/revoke,
//     DELETE /connected_accounts/{id}
//   - Toolkits      — GET /toolkits, GET /toolkits/{slug}
//   - Tool Execute  — POST /tools/execute/{tool_slug}
//   - Webhook       — HMAC-SHA256 signature verification
//
// More surface (auth configs, triggers, proxy execute, etc.) can be
// added later without changing the existing types.
//
// # Quick start
//
//	client, err := composio.NewClient(composio.Options{
//	    APIKey: os.Getenv("COMPOSIO_API_KEY"),
//	})
//	if err != nil { return err }
//
//	link, err := client.CreateLink(ctx, composio.CreateLinkRequest{
//	    AuthConfigID: "ac_abc",
//	    UserID:       "u_123",
//	    CallbackURL:  "https://app.example.com/composio/callback",
//	})
//	// redirect user to link.RedirectURL
//
//	session, err := client.CreateSession(ctx, composio.CreateSessionRequest{
//	    UserID: "u_123",
//	})
//	// agent runtime now consumes session.MCP.URL + composio.MCPAuthHeaders(...)
//
// # Errors
//
// All non-2xx responses come back as a *APIError carrying the upstream
// status, slug, and message. Transport errors come back unwrapped from
// resty so callers can errors.Is/As as usual.
//
// # Webhook verification
//
// [VerifyWebhook] verifies the HMAC-SHA256 signature Composio attaches
// to every webhook delivery, with a configurable replay tolerance.
// See https://docs.composio.dev/docs/setting-up-triggers/subscribing-to-events#verifying-signatures
package composio

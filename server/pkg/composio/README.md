# composio

A small, standalone Go SDK for the [Composio v3.1 REST API](https://docs.composio.dev/api-reference).

This package is intentionally self-contained — its only third-party dependency
is [`github.com/go-resty/resty/v2`](https://github.com/go-resty/resty). It does
not import any other Multica package, so it can be reused by other services or
extracted into its own module unchanged.

## Scope (MVP)

Only the endpoints required by the first-stage Composio integration are wired
up. More surface (auth configs, triggers, proxy execute, etc.) can be added
later without changing existing types.

| Capability | Method | REST endpoint |
| --- | --- | --- |
| Create Connect Link (hosted auth flow) | `Client.CreateLink` | `POST /connected_accounts/link` |
| Create MCP / tool-router session | `Client.CreateSession` | `POST /tool_router/session` |
| List connected accounts (per user) | `Client.ListConnectedAccounts` | `GET /connected_accounts` |
| Revoke a connection at the provider | `Client.RevokeConnection` | `POST /connected_accounts/{id}/revoke` |
| Delete a connection record (idempotent) | `Client.DeleteConnectedAccount` | `DELETE /connected_accounts/{id}` |
| List toolkits | `Client.ListToolkits` | `GET /toolkits` |
| Get a toolkit by slug | `Client.GetToolkit` | `GET /toolkits/{slug}` |
| Execute a tool deterministically | `Client.ExecuteTool` | `POST /tools/execute/{slug}` |
| Verify a webhook delivery | `VerifyWebhook` / `VerifyHTTPRequest` | (offline) |

## Quick start

```go
import (
    "context"
    "os"

    "github.com/multica-ai/multica/server/pkg/composio"
)

client, err := composio.NewClient(composio.Options{
    APIKey: os.Getenv("COMPOSIO_API_KEY"),
})
if err != nil { /* ... */ }

// 1. Send a user to the hosted Connect Link
link, err := client.CreateLink(ctx, composio.CreateLinkRequest{
    AuthConfigID: "ac_xxxxxxxx",          // configured in the Composio dashboard
    UserID:       multicaUserID.String(), // your own user id
    CallbackURL:  "https://app.multica.ai/api/integrations/composio/callback",
})
// → http.Redirect(w, r, link.RedirectURL, http.StatusFound)

// 2. After Composio creates the account, fetch what the user has connected
accounts, err := client.ListConnectedAccounts(ctx, composio.ListConnectedAccountsRequest{
    UserIDs:  []string{multicaUserID.String()},
    Statuses: []string{"ACTIVE"},
})

// 3. Open an MCP session for the agent runtime
session, err := client.CreateSession(ctx, composio.CreateSessionRequest{
    UserID: multicaUserID.String(),
    ManageConnections: &composio.ManageConnections{
        CallbackURL: "https://app.multica.ai/settings/integrations",
    },
})
mcpURL  := session.MCP.URL
mcpHdr  := client.MCPAuthHeaders() // {"x-api-key": "..."} – attach to MCP client

// 4. Disconnect (idempotent — 404 returns nil)
_ = client.RevokeConnection(ctx, "ca_xxxxxxxx")
_ = client.DeleteConnectedAccount(ctx, "ca_xxxxxxxx")
```

## Webhook verification

```go
secret := os.Getenv("COMPOSIO_WEBHOOK_SECRET")

http.HandleFunc("/api/integrations/composio/webhook", func(w http.ResponseWriter, r *http.Request) {
    body, err := composio.VerifyHTTPRequest(secret, r, composio.VerifyOptions{})
    if err != nil {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }
    event, err := composio.ParseEvent(body)
    if err != nil {
        http.Error(w, "bad payload", http.StatusBadRequest)
        return
    }
    switch event.Type {
    case "composio.connected_account.expired":
        // mark row as expired, notify the user, ...
    }
    w.WriteHeader(http.StatusOK)
})
```

`VerifyWebhook` enforces a 300-second replay tolerance by default (matching
Composio's official SDKs). Pass `VerifyOptions{Tolerance: ...}` to tune it, or
`-1` to disable the check entirely (only useful when replaying historical
deliveries in tests).

The `webhook-signature` header is parsed as a list of `<version>,<sig>` pairs
so future signing versions don't break verification.

## Errors

All non-2xx responses are returned as a `*composio.APIError` carrying the
upstream status, slug, and message:

```go
_, err := client.CreateLink(ctx, req)
var apiErr *composio.APIError
if errors.As(err, &apiErr) {
    if apiErr.IsRateLimited() { /* back off */ }
    log.Printf("composio: %d %s (%s) req=%s", apiErr.HTTPStatus, apiErr.Message, apiErr.Slug, apiErr.RequestID)
}
```

`DeleteConnectedAccount` deliberately swallows 404 so the operation is
idempotent — every other error is propagated unchanged.

## Testing

The SDK is exercised entirely against `httptest.NewServer` so unit tests run
offline. Run them with:

```
go test ./server/pkg/composio/...
```

Current coverage: **82.2 %**.

## Design notes

- **Standalone.** Zero coupling to Multica internals — depend on this package
  from `server/internal/integrations/composio` (Stage 2 integration glue) or
  anywhere else without circular-import risk.
- **`x-api-key`, not Bearer.** Composio's v3.1 REST API authenticates with an
  `x-api-key` header. The SDK sets it on every request and exposes
  `Client.APIKeyHeader()` / `Client.MCPAuthHeaders()` so callers know
  which header to attach when they're reaching Composio outside the SDK
  (e.g. the MCP streaming client in the agent runtime).
- **Loose typing for evolving fields.** Session request blocks (`toolkits`,
  `auth_configs`, `tools`, `multi_account`, …) and tool execution arguments
  use `map[string]any` because their nested schemas are large and likely to
  evolve. The frequently-used `manage_connections` block has a typed
  wrapper — extend the typed surface as more shapes stabilise.
- **Webhook signing matches the official SDKs.** HMAC-SHA256 over
  `{id}.{timestamp}.{rawBody}`, base64-encoded, with a 300-second replay
  window. See
  [Composio webhook verification](https://docs.composio.dev/docs/setting-up-triggers/subscribing-to-events#verifying-signatures).

## Roadmap (out of scope for v1)

- Auth-config CRUD (`/auth_configs`)
- Triggers (`/triggers`)
- Proxy execute (`/tools/execute/proxy`)
- Session meta-tool / `attach` / `search` endpoints
- Pagination iterators
- Built-in retry middleware on 429 / 5xx

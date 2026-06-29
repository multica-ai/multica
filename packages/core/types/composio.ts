/** A Composio toolkit as surfaced by GET /api/integrations/composio/toolkits.
 *
 * Wire shape mirrors `ComposioToolkitResponse` in
 * `server/internal/handler/integrations_composio.go`. New fields the backend
 * adds later MUST stay optional so older desktop builds keep parsing — see
 * CLAUDE.md → API Response Compatibility. */
export interface ComposioToolkit {
  slug: string;
  name: string;
  logo?: string;
  category?: string;
  /** Whether the project has an enabled auth config for this toolkit. When
   * false the UI must not offer a working Connect button — BeginConnect would
   * 400 with "toolkit not supported". */
  connectable: boolean;
}

/** A user's Composio connected account, as returned by
 * GET /api/integrations/composio/connections. Mirrors
 * `ComposioConnectionResponse` server-side. */
export interface ComposioConnection {
  id: string;
  toolkit_slug: string;
  /** Connection lifecycle state. `expired` surfaces a Reconnect affordance in
   * the UI; the backend only starts emitting it once Stage 4 webhook handling
   * lands (MUL-3719), but the client renders the branch ahead of that. */
  status: "active" | "expired" | "revoked" | string;
  connected_at: string;
  last_used_at?: string | null;
}

/** Response of POST /api/integrations/composio/connect/init — the hosted
 * Composio Connect Link the browser is redirected to. */
export interface ComposioConnectInitResponse {
  redirect_url: string;
}

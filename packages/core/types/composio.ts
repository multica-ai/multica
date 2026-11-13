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
  /** Whether the project has an enabled auth config for this toolkit. Since
   * MUL-4009 the backend only returns connectable toolkits, so this is always
   * true on the wire; the field is kept for backward compatibility with older
   * desktop builds that branch on it. The UI still guards the Connect button on
   * it as a client-side backstop. */
  connectable: boolean;
}

/** A user's Composio connected account, as returned by
 * GET /api/integrations/composio/connections. Mirrors
 * `ComposioConnectionResponse` server-side. */
export interface ComposioConnection {
  id: string;
  toolkit_slug: string;
  /** The backend does not yet emit "expired"; the reconnect UI is prepared for it. */
  status: "active" | "expired" | "revoked" | string;
  connected_at: string;
  last_used_at?: string | null;
}

/** Response of POST /api/integrations/composio/connect/init — the hosted
 * Composio Connect Link the browser is redirected to. */
export interface ComposioConnectInitResponse {
  redirect_url: string;
}

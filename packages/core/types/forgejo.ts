/**
 * Forgejo (self-hosted Git forge) integration types. Unlike GitHub there is no
 * App/installation model: each workspace stores a token-based connection to a
 * Forgejo instance. Pull requests mirrored from Forgejo are surfaced through
 * the shared GitHubPullRequest shape (tagged with `provider: "forgejo"`).
 */

export interface ForgejoConnection {
  id: string;
  workspace_id: string;
  /** Instance base URL, e.g. https://forgejo.example.com (no trailing slash). */
  instance_url: string;
  /** Login (user or org) the stored access token authenticates as. */
  account_login: string;
  /** Absolute webhook endpoint to register on Forgejo. Empty when the server
   * has no public URL configured; the UI then prefixes `webhook_path`. */
  webhook_url: string;
  webhook_path: string;
  created_at: string;
}

export interface ListForgejoConnectionsResponse {
  connections: ForgejoConnection[];
  /** Whether the deployment has MULTICA_FORGEJO_SECRET_KEY configured. When
   * false the connect form is disabled. Older backends omit it; treat absence
   * as false for read-only safety. */
  configured?: boolean;
  /** Whether the caller can connect / disconnect. Non-admins get false. */
  can_manage?: boolean;
}

export interface ConnectForgejoRequest {
  instance_url: string;
  access_token: string;
}

export interface ConnectForgejoResponse extends ForgejoConnection {
  /** One-time plaintext webhook secret to paste into Forgejo. Not retrievable
   * afterwards (stored encrypted); reconnecting rotates it. */
  webhook_secret: string;
}

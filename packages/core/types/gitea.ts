export interface GiteaConnection {
  id: string;
  workspace_id: string;
  /** The instance web base URL, e.g. `https://gitea.example.com`. */
  base_url: string;
  account_login: string;
  account_avatar_url: string | null;
  created_at: string;
}

export interface ListGiteaConnectionsResponse {
  connections: GiteaConnection[];
  /** Whether the deployment has the Gitea secret key configured. When false, the Connect form is disabled. */
  configured: boolean;
  /** Whether the caller can connect / disconnect. Non-admin members get `false`.
   * Older backends omit the field; treat absence as `false` for read-only safety. */
  can_manage?: boolean;
  /** Fully-qualified webhook endpoint to paste into each Gitea repo
   * (`<MULTICA_PUBLIC_URL>/api/webhooks/gitea`). Null when the deployment has no
   * public URL configured; the UI then shows the path only. */
  webhook_url?: string | null;
  /** Whether `MULTICA_GITEA_WEBHOOK_SECRET` is set. When false the endpoint
   * returns 503, so the UI warns that PR mirroring will not work yet. */
  webhook_configured?: boolean;
}

export interface GiteaConnectRequest {
  base_url: string;
  token: string;
}

/**
 * Jira Cloud integration types. Each workspace stores token-based connections
 * to Jira Cloud sites (base URL + account email + API token). Issues created
 * from Jira webhooks surface as regular Multica issues with origin_type
 * 'jira'. Mirrors the VCS connection shapes (see vcs.ts).
 */

export interface JiraConnection {
  id: string;
  workspace_id: string;
  /** Jira Cloud site base URL, e.g. https://acme.atlassian.net (no trailing slash). */
  base_url: string;
  /** Email of the Atlassian account the stored API token authenticates as. */
  account_email: string;
  /** Absolute inbound webhook endpoint to register on the Jira site. Empty when
   * the server has no public URL configured; the UI then prefixes `webhook_path`. */
  webhook_url: string;
  webhook_path: string;
  /** JQL filter used by the pull-based sync. Empty means the server default
   * `assignee = currentUser()` is applied at sync time. */
  jql: string;
  created_at: string;
}

export interface ListJiraConnectionsResponse {
  connections: JiraConnection[];
  /** Whether the deployment has MULTICA_JIRA_SECRET_KEY configured. When false
   * the connect form is disabled. Older backends omit it; treat as false. */
  configured?: boolean;
  /** Whether the caller can connect / disconnect. Non-admins get false. */
  can_manage?: boolean;
}

export interface ConnectJiraRequest {
  base_url: string;
  account_email: string;
  api_token: string;
  /** Optional JQL filter for the pull-based sync. */
  jql?: string;
}

/** Summary returned by POST .../jira/connections/{id}/sync — the pull-based
 * import for accounts that cannot register Jira webhooks. */
export interface SyncJiraConnectionResponse {
  created: number;
  updated: number;
  /** How many Jira issues the JQL search returned (created + updated + skipped). */
  total: number;
}

export interface ConnectJiraResponse extends JiraConnection {
  /** One-time plaintext webhook secret to configure on the Jira webhook
   * (X-Multica-Webhook-Secret header or `secret` query parameter on the
   * webhook URL). Not retrievable afterwards (stored encrypted);
   * reconnecting the same site rotates it. */
  webhook_secret: string;
}

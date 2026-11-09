/** Wire shapes of the Connected Apps API (MUL-8516). Mirrors the response
 * types in `server/internal/handler/connected_apps.go`. New backend fields
 * must stay optional here so older desktop builds keep parsing. */

/** Whose account an agent's app acts as: the workspace's team account, or
 * the personal account of whoever triggered the run. */
export type AppAccountScope = "team" | "personal";

/** How far an agent may go with an app. The saved `confirm_writes` value
 * is accepted for compatibility and now runs directly like `write`. */
export type AppAccessLevel = "read" | "confirm_writes" | "write";

export type AppActionClass = "read" | "write" | "send" | "delete" | "share";

/** Saved `confirm` rules now allow calls directly. */
export type AppActionPolicy = "allow" | "confirm" | "block";

/** `all` lists every connectable app; `curated` only Multica's reviewed set;
 * `custom` only the apps an admin listed. */
export type AppCatalogMode = "all" | "curated" | "custom";

export interface AppConnection {
  id: string;
  toolkit_slug: string;
  status: "active" | "expired" | "revoked" | string;
  connected_by?: string | null;
  connected_at: string;
  last_used_at: string | null;
}

export interface AgentApp {
  agent_id: string;
  toolkit_slug: string;
  account_scope: AppAccountScope;
  level: AppAccessLevel;
}

export interface AppsOverview {
  catalog_mode: AppCatalogMode;
  /** The apps of a custom catalog. */
  allowed_toolkits?: string[];
  /** Whether the caller administers team accounts and catalog settings. */
  can_manage: boolean;
  team: AppConnection[];
  personal: AppConnection[];
  agent_apps: AgentApp[];
}

export interface AppCatalogEntry {
  slug: string;
  name: string;
  logo?: string;
  category?: string;
  curated: boolean;
  recommended_scope?: AppAccountScope;
  /** False only in the admin's full view for apps the catalog leaves out. */
  allowed?: boolean;
}

/** One tool of an app and how this workspace classifies it. */
export interface AppTool {
  slug: string;
  name: string;
  description: string;
  auto_class: AppActionClass;
  class: AppActionClass;
  overridden: boolean;
}

export type AppCallOutcome =
  | "ok"
  | "error"
  | "blocked"
  | "pending"
  | "approved"
  | "rejected"
  | "rate_limited";

/** One call an issue's runs made through an app. */
export interface IssueAppCall {
  id: string;
  task_id: string | null;
  agent_id: string;
  account_scope: AppAccountScope;
  account_owner_user_id: string | null;
  toolkit_slug: string;
  tool_slug: string;
  action_class: AppActionClass | string;
  outcome: AppCallOutcome | string;
  request_id: string | null;
  /** Present only for the account's owner. */
  error: string | null;
  created_at: string;
}

export interface AppCatalogResponse {
  catalog_mode: AppCatalogMode;
  apps: AppCatalogEntry[];
}

export interface AppRule {
  action_class: AppActionClass;
  policy: AppActionPolicy;
  default_policy: AppActionPolicy;
  custom: boolean;
}

export interface AppToolCall {
  id: string;
  agent_id: string;
  issue_id: string | null;
  chat_session_id: string | null;
  triggered_by: string | null;
  tool_slug: string;
  action_class: AppActionClass;
  outcome: AppCallOutcome | string;
  error: string | null;
  created_at: string;
}

export interface TeamAppImpact {
  agents: { id: string; name: string }[];
  active_tasks: number;
  active_autopilots: number;
  pending_requests: number;
}

export type AppRequestStatus =
  | "pending"
  | "approved"
  | "rejected"
  | "failed"
  | "expired"
  | "cancelled";

/** A call waiting for (or decided by) a person. Arguments and results are
 * present only for the approver; everyone else sees `redacted: true`. */
export interface AppRequest {
  id: string;
  kind: "action" | "connect";
  status: AppRequestStatus | string;
  agent_id: string;
  issue_id: string | null;
  chat_session_id: string | null;
  approver_user_id: string;
  account_scope: AppAccountScope;
  toolkit_slug: string;
  tool_slug: string;
  action_class: AppActionClass | string;
  summary: string;
  arguments?: Record<string, unknown>;
  approved_arguments?: Record<string, unknown>;
  result?: unknown;
  error: string | null;
  reason: string | null;
  redacted: boolean;
  expires_at: string;
  decided_by: string | null;
  decided_at: string | null;
  created_at: string;
}

/** One page of requests. `next_before` pages back through decided history. */
export interface AppRequestList {
  requests: AppRequest[];
  next_before: string | null;
}

export interface UpdateAgentAppsRequest {
  apps: { toolkit_slug: string; account_scope: AppAccountScope; level: AppAccessLevel }[];
}

/** `app_request:changed` realtime payload: ids and status only. */
export interface AppRequestChangedPayload {
  request_id: string;
  issue_id: string | null;
  chat_session_id: string | null;
  status: string;
}

/**
 * A plugin relates to Multica in exactly three ways: Action (plugin calls
 * Multica), Hook (Multica calls plugin), and Resource (a static contribution
 * with no call at all). These types mirror the manifest contract the server
 * parses; the client never re-derives them from anything else.
 */

export type PluginConfigFieldType = "string" | "number" | "bool" | "enum" | "secret";

export interface PluginConfigField {
  key: string;
  type: PluginConfigFieldType | string;
  label: string;
  description?: string;
  required: boolean;
  options?: string[];
  placeholder?: string;
  /** String fields only: render a textarea instead of a single-line input. */
  multiline?: boolean;
}

export type PluginSurfaceType = "issue_panel" | "sidebar_panel" | "modal";

export interface PluginSurface {
  key: string;
  type: PluginSurfaceType | string;
  name: string;
  entry: string;
  platforms?: string[];
}

export type PluginHookTrigger = "ui" | "manual" | "agent" | "event";

export interface PluginHook {
  key: string;
  name: string;
  description: string;
  triggers: (PluginHookTrigger | string)[];
  events?: string[];
  transport: string;
}

export interface PluginResource {
  type: string;
  key: string;
  entry: string;
}

export interface PluginInstallation {
  id: string;
  plugin_key: string;
  name: string;
  description?: string;
  version: string;
  source_url: string;
  enabled: boolean;
  granted_scopes: string[];
  config_schema: PluginConfigField[];
  /** Non-secret values only. A secret is never returned by any endpoint. */
  config: Record<string, unknown>;
  /** Names of secret fields that hold a value — never the values themselves. */
  configured_secrets: string[];
  surfaces: PluginSurface[];
  hooks: PluginHook[];
  resources: PluginResource[];
  created_at: string;
  updated_at: string;
}

export interface PluginInstallationListResponse {
  plugins: PluginInstallation[];
}

export interface PluginManifestSummary {
  key: string;
  name: string;
  description?: string;
  version: string;
  author: { name: string; url?: string };
}

/**
 * What the consent screen renders. There is no signature and no trust tier in
 * this model: an administrator reading the scope list IS the trust decision.
 */
export interface PluginPreview {
  manifest: PluginManifestSummary;
  scopes: string[];
  config_schema: PluginConfigField[];
  installed: boolean;
  installed_version?: string;
  /** Scopes this install would add on top of what is already granted. */
  added_scopes?: string[];
}

export interface PluginPreviewRequest {
  source_url: string;
}

export interface PluginInstallRequest {
  source_url: string;
  granted_scopes: string[];
}

export interface PluginConfigRequest {
  values: Record<string, unknown>;
}

/** One completed hook call, as returned to whoever invoked it. */
export interface PluginHookResult {
  /**
   * The HOST's classification, not the endpoint's. "refused" means the call was
   * never made — a scope, a rate limit or a disabled install — which is a
   * different problem for the reader than an endpoint that answered badly.
   */
  status: string;
  output?: unknown;
  error?: string;
  latency_ms: number;
  hook_key: string;
  trigger: string;
  attempts: number;
}

/**
 * One recorded hook call. Operational telemetry, TTL-swept, and deliberately
 * carrying no request or response body — a table that kept every payload would
 * be a second copy of workspace content with none of its deletion paths.
 */
export interface PluginInvocation {
  id: string;
  hook_key: string;
  trigger: string;
  status: string;
  event_type?: string;
  attempt: number;
  latency_ms: number;
  error?: string;
  created_at: string;
}

/** Both values are shown once, by the request that minted them. */
export interface PluginTokenIssue {
  token: string;
  signing_secret: string;
}

/**
 * One tool an `mcp`-transport hook's server currently offers.
 *
 * `approved` is the administrator's pin. `drifted` means the tool IS approved
 * but its schema no longer matches what was approved — surfaced rather than
 * silently re-approved, because the administrator approved a specific shape and
 * a changed one is a new decision.
 */
export interface PluginMCPTool {
  name: string;
  description: string;
  schema_digest: string;
  approved: boolean;
  drifted: boolean;
}

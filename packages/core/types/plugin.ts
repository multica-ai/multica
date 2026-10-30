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

export type PluginHookTrigger = "ui" | "manual" | "agent" | "event" | "schedule";

export interface PluginHookSchedule {
  cron: string;
  timezone: string;
  /** Display-only projection. Runtime correctness never depends on this value. */
  next_run_at?: string;
}

export interface PluginHook {
  key: string;
  name: string;
  description: string;
  triggers: (PluginHookTrigger | string)[];
  events?: string[];
  schedule?: PluginHookSchedule;
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
  /**
   * The published version this installation is bound to. Publishing a new
   * version does not touch it — upgrading is an explicit second consent, which
   * is what makes "the admin approved this code" true rather than aspirational.
   */
  package_version_id: string;
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
  contributes?: {
    hooks?: Array<{
      key: string;
      name: string;
      triggers: (PluginHookTrigger | string)[];
      schedule?: PluginHookSchedule;
    }>;
  };
}

/**
 * What the consent screen renders. There is no signature and no trust tier in
 * this model: an administrator reading the scope list IS the trust decision.
 *
 * It describes one published version, and installing names that same version.
 */
export interface PluginPreview {
  manifest: PluginManifestSummary;
  scopes: string[];
  config_schema: PluginConfigField[];
  version_id: string;
  version: string;
  digest: string;
  installed: boolean;
  installed_version?: string;
  /** Scopes this install would add on top of what is already granted. */
  added_scopes?: string[];
}

export interface PluginPreviewRequest {
  version_id: string;
}

export interface PluginInstallRequest {
  version_id: string;
  granted_scopes: string[];
}

/** One immutable published version of a plugin package. */
export interface PluginPackageVersion {
  id: string;
  version: string;
  /** sha256 of the artifact's contents, so two people can confirm they match. */
  digest: string;
  size_bytes: number;
  published_at: string;
  /** True for the version this workspace currently runs, if any. */
  installed: boolean;
}

/**
 * A plugin published into this workspace. Publishing is workspace-private: a
 * public directory needs review, reporting and takedown, which is a separate
 * decision from where the artifact lives.
 */
export interface PluginPackage {
  id: string;
  plugin_key: string;
  name: string;
  /** Newest first. */
  versions: PluginPackageVersion[];
  created_at: string;
}

export interface PluginPackageListResponse {
  packages: PluginPackage[];
}

/** One short-lived, installation-bound launch of a hosted surface. */
export interface PluginSurfaceLaunch {
  /** Multica's cookie-free content URL, never the plugin author's server. */
  url: string;
  /** Single-use proof the generated document must present to this frame. */
  bridge_token: string;
  version: string;
  digest: string;
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
  delivery_id?: string;
  planned_at?: string;
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

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

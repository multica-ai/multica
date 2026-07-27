import {
  DEFAULT_RUNTIME_CONFIG,
  urlsMatch,
  type RuntimeConfigResult,
} from "./runtime-config";

export interface EnvironmentHint {
  name: string;
  apiUrl: string;
}

/**
 * Desktop multi-server environment cue. Returns null for Multica Cloud so the
 * common single-tenant SaaS case stays visually quiet; self-host / custom
 * backends surface their display name.
 */
export function resolveEnvironmentHint(
  result: RuntimeConfigResult,
): EnvironmentHint | null {
  if (!result.ok) return null;
  const active =
    result.servers.servers.find((s) => s.id === result.servers.activeServerId) ??
    null;
  if (!active) return null;
  if (urlsMatch(active.apiUrl, DEFAULT_RUNTIME_CONFIG.apiUrl)) return null;
  return { name: active.name, apiUrl: active.apiUrl };
}

const TITLE_SEP = " · ";

/** Strip a previously applied environment suffix so we never double-append. */
export function stripEnvironmentTitleSuffix(title: string, name: string): string {
  const suffix = `${TITLE_SEP}${name}`;
  return title.endsWith(suffix) ? title.slice(0, -suffix.length) : title;
}

export function withEnvironmentTitleSuffix(title: string, name: string): string {
  const base = stripEnvironmentTitleSuffix(title, name).trim() || "Multica";
  return `${base}${TITLE_SEP}${name}`;
}

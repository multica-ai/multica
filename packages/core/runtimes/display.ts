import type { AgentRuntime } from "../types";

/**
 * The name to show for a runtime (MUL-4217): the user's custom override when
 * set, otherwise the daemon-proposed default. Defends against older backends
 * that omit custom_name and against whitespace-only overrides.
 */
export function runtimeDisplayName(
  runtime: Pick<AgentRuntime, "name" | "custom_name">,
): string {
  const custom = runtime.custom_name?.trim();
  return custom ? custom : runtime.name;
}

/**
 * A runtime label that always surfaces the provider family, even when a custom
 * alias is set (#5260). The daemon bakes the provider into `name`
 * ("Codex (host)"), but a user alias replaces that whole string via
 * runtimeDisplayName and hides which CLI actually backs the runtime. When an
 * alias is present we re-attach the provider in parentheses; without one `name`
 * already carries it, so we return it unchanged to avoid a duplicated provider
 * ("Codex (host) (codex)").
 */
export function runtimeDisplayLabel(
  runtime: Pick<AgentRuntime, "name" | "custom_name" | "provider">,
): string {
  const display = runtimeDisplayName(runtime);
  const hasCustom = !!runtime.custom_name?.trim();
  const provider = runtime.provider?.trim();
  if (!hasCustom || !provider) return display;
  return `${display} (${providerDisplayName(provider)})`;
}

/**
 * Human display names for runtime provider slugs. The daemon bakes these into
 * `name` for the no-alias case ("Trae (host)"), so we mirror them here to keep
 * the aliased label consistent — e.g. `traecli` must read "Trae", and mixed-case
 * families like CodeBuddy / OpenCode / OpenClaw must not be flattened by naive
 * title-casing (#5260). Kept in sync with the ProviderLogo switch in
 * packages/views/runtimes/components/provider-logo.tsx.
 */
const PROVIDER_DISPLAY_NAMES: Record<string, string> = {
  claude: "Claude",
  codebuddy: "CodeBuddy",
  codex: "Codex",
  opencode: "OpenCode",
  openclaw: "OpenClaw",
  hermes: "Hermes",
  pi: "Pi",
  copilot: "Copilot",
  cursor: "Cursor",
  kimi: "Kimi",
  kiro: "Kiro",
  qoder: "Qoder",
  antigravity: "Antigravity",
  traecli: "Trae",
};

/**
 * Map a provider slug to its display name, falling back to a title-cased slug
 * for providers not in the table.
 */
export function providerDisplayName(provider: string): string {
  const known = PROVIDER_DISPLAY_NAMES[provider];
  if (known) return known;
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}

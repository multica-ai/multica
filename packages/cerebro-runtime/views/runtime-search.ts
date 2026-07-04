import type { AgentRuntime } from "@multica/core/types/agent";

// FIR-2669: full-field search for the Runtimes list. Jesper asked search to
// match every field the table can show, except the ones a text search can't
// meaningfully cover (the agent-avatar stack and the numeric cost delta). The
// page resolves the row-derived strings (account identity, owner name, health
// label) and passes them in as `extras`, so this stays a pure, testable
// function with no query/i18n dependency.

export interface RuntimeSearchExtras {
  /** cerebro_account login identity + provider, already resolved for the row. */
  accountLabel?: string | null;
  /** Owner member display name, if the row has one. */
  ownerName?: string | null;
  /** Derived health label in the current locale (e.g. "Online", "Offline"). */
  healthLabel?: string | null;
}

/** Read the agent-native CLI/tool version off a runtime's loose metadata bag. */
function runtimeVersions(runtime: AgentRuntime): string {
  const meta = runtime.metadata as Record<string, unknown> | null;
  const parts: string[] = [];
  if (meta) {
    if (typeof meta.version === "string") parts.push(meta.version);
    if (typeof meta.cli_version === "string") parts.push(meta.cli_version);
  }
  return parts.join(" ");
}

/**
 * True when `query` (already trimmed + lowercased by the caller, or raw — we
 * normalise defensively) matches any searchable field of the runtime row.
 * Empty query matches everything.
 */
export function matchesRuntimeSearch(
  runtime: AgentRuntime,
  query: string,
  extras: RuntimeSearchExtras = {},
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const haystack = [
    runtime.name,
    runtime.provider,
    runtime.device_info ?? "",
    runtime.runtime_mode,
    runtimeVersions(runtime),
    extras.accountLabel ?? "",
    extras.ownerName ?? "",
    extras.healthLabel ?? "",
  ]
    .join(" ")
    .toLowerCase();
  return haystack.includes(q);
}

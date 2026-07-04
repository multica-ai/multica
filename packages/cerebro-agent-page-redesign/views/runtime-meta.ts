import type { AgentRuntime } from "@multica/core/types";

/**
 * Read the CLI/runtime version off a runtime's loose `metadata` bag. The daemon
 * reports it under free-form keys that differ per provider, so we probe the
 * common ones (`version`, then `cli_version`) and return null rather than
 * inventing a value when nothing usable is present. Defensive by contract: the
 * metadata bag is untyped JSON from the wire (see API Response Compatibility).
 */
export function runtimeVersion(runtime: AgentRuntime | null): string | null {
  const meta = runtime?.metadata;
  if (!meta || typeof meta !== "object") return null;
  const bag = meta as Record<string, unknown>;
  const v = bag.version ?? bag.cli_version;
  return typeof v === "string" && v.length > 0 ? v : null;
}

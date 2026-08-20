import type { LiteLlmKey } from "./litellm-schema";

/**
 * Pure matching logic, split out of litellm-join.ts so it's testable
 * without pulling in `server-only` (importing litellm-join.ts from a test
 * throws by design — see lib/sql-guard.ts's own comment on that package).
 *
 * Gandalf always names the key `agentfarm-<slug>` (see
 * workspace-create.service.ts's mintLlmProxyKey) regardless of when the
 * workspace was created, so match on key_alias rather than
 * `metadata.workspace_slug` — that metadata field is only stamped by
 * Gandalf's current minting path and is absent on most existing keys,
 * which made the join silently fail for nearly every bot-owned workspace.
 */
export function findKeyForSlug(keys: LiteLlmKey[], slug: string): LiteLlmKey | null {
  return keys.find((k) => k.key_alias === `agentfarm-${slug}`) ?? null;
}

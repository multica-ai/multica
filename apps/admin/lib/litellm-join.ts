import "server-only";
import { listLiteLlmKeys } from "./litellm";
import type { LiteLlmKey } from "./litellm-schema";
import type { WorkspaceListItem } from "./types";

/**
 * Join strategy for LiteLLM data (documented gap — no DB column links a
 * workspace to a LiteLLM key/team). Keys are created by Gandalf's
 * workspace_create command with key_alias `agentfarm-<slug>` (not equal to
 * the slug itself), so the reliable match is `metadata.workspace_slug`,
 * which Gandalf stamps on every key it creates. When nothing matches,
 * llmKey/team stay null and the UI renders "No LiteLLM key linked" — never
 * an invented value.
 */
export function findKeyForSlug(keys: LiteLlmKey[], slug: string): LiteLlmKey | null {
  return keys.find((k) => k.metadata?.workspace_slug === slug) ?? null;
}

export async function attachLiteLlmToList(
  items: WorkspaceListItem[],
): Promise<WorkspaceListItem[]> {
  const keys = await listLiteLlmKeys();
  if (keys.length === 0) return items;
  return items.map((item) => {
    const match = findKeyForSlug(keys, item.slug);
    if (!match) return item;
    return { ...item, llmKey: match.key_alias ?? null, team: match.team_alias ?? null };
  });
}

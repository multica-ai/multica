import "server-only";
import { listLiteLlmKeys } from "./litellm";
import type { LiteLlmKey } from "./litellm-schema";
import type { WorkspaceListItem } from "./types";

/**
 * No DB column links a workspace to a LiteLLM key/team. Gandalf stamps
 * `metadata.workspace_slug` on every key it creates (key_alias is
 * `agentfarm-<slug>`, not the slug itself), so that's the reliable match.
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

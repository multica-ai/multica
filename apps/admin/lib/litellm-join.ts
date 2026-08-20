import "server-only";
import { listLiteLlmKeys } from "./litellm";
import { findKeyForSlug } from "./litellm-match";
import type { WorkspaceListItem } from "./types";

export { findKeyForSlug } from "./litellm-match";

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

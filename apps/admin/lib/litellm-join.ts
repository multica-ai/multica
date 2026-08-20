import "server-only";
import { listLiteLlmKeys, listLiteLlmTeams } from "./litellm";
import { findKeyForSlug, resolveTeamName } from "./litellm-match";
import type { WorkspaceListItem } from "./types";

export { findKeyForSlug, resolveTeamName } from "./litellm-match";

export async function attachLiteLlmToList(
  items: WorkspaceListItem[],
): Promise<WorkspaceListItem[]> {
  const [keys, teams] = await Promise.all([listLiteLlmKeys(), listLiteLlmTeams()]);
  if (keys.length === 0) return items;
  return items.map((item) => {
    const match = findKeyForSlug(keys, item.slug);
    if (!match) return item;
    return {
      ...item,
      llmKey: match.key_alias ?? null,
      team: resolveTeamName(teams, match.team_id),
      keySpend: match.spend ?? null,
    };
  });
}

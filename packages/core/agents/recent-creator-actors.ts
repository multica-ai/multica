import { MAX_RECENT_AGENTS } from "./recent-agent-selection";

export type CreatorActorRef = {
  type: "agent" | "squad";
  id: string;
};

export function creatorActorsStorageKey(userId: string, wsId: string): string {
  return `multica.recent-creator-actors.${userId}.${wsId}`;
}

export function parseCreatorActors(raw: string | null): CreatorActorRef[] {
  if (!raw) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    const seen = new Set<string>();
    const result: CreatorActorRef[] = [];
    for (const item of parsed) {
      if (!isCreatorActorRef(item)) continue;
      const key = `${item.type}:${item.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      result.push({ type: item.type, id: item.id });
      if (result.length === MAX_RECENT_AGENTS) break;
    }
    return result;
  } catch {
    return [];
  }
}

export function prependCreatorActor(
  current: readonly CreatorActorRef[],
  next: CreatorActorRef,
  max = MAX_RECENT_AGENTS,
): CreatorActorRef[] {
  const result: CreatorActorRef[] = [
    next,
    ...current.filter(
      (item) => !(item.type === next.type && item.id === next.id),
    ),
  ];
  return result.slice(0, max);
}

/**
 * Creator-picker recents: local mixed agent/squad picks first (recency),
 * then server recent agents that are not already listed.
 */
export function mergeRecentCreatorActors(
  local: readonly CreatorActorRef[],
  recentAgentIds: readonly string[],
  max = MAX_RECENT_AGENTS,
): CreatorActorRef[] {
  const seen = new Set<string>();
  const result: CreatorActorRef[] = [];
  const push = (item: CreatorActorRef) => {
    const key = `${item.type}:${item.id}`;
    if (seen.has(key)) return;
    seen.add(key);
    result.push(item);
  };
  for (const item of local) push(item);
  for (const id of recentAgentIds) push({ type: "agent", id });
  return result.slice(0, max);
}

function isCreatorActorRef(value: unknown): value is CreatorActorRef {
  if (value === null || typeof value !== "object") return false;
  const record = value as { type?: unknown; id?: unknown };
  return (
    (record.type === "agent" || record.type === "squad") &&
    typeof record.id === "string" &&
    record.id.length > 0
  );
}

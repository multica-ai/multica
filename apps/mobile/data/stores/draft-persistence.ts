import AsyncStorage from "@react-native-async-storage/async-storage";

const DRAFT_PREFIX = "multica_draft:";

export function draftStorageKey(
  kind: "chat" | "new-issue" | "new-project" | "outbox",
  userId: string,
  workspaceSlug: string,
) {
  return `${DRAFT_PREFIX}${kind}:${userId}:${workspaceSlug}`;
}

/**
 * Read a target partition before switching a Zustand persist store to it.
 *
 * Calling `store.setState` after changing persist's `name` writes through to
 * that new name. Reading first prevents a scope switch from replacing an
 * existing partition with the previous scope's empty in-memory state.
 */
export async function readDraftPartition<T extends object>(
  kind: "chat" | "new-issue" | "new-project" | "outbox",
  userId: string,
  workspaceSlug: string,
): Promise<T | null> {
  const raw = await AsyncStorage.getItem(draftStorageKey(kind, userId, workspaceSlug));
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      !parsed ||
      typeof parsed !== "object" ||
      !("state" in parsed) ||
      !parsed.state ||
      typeof parsed.state !== "object"
    ) {
      return null;
    }
    return parsed.state as T;
  } catch {
    return null;
  }
}

function isOwnedDraftKey(key: string, userId: string): boolean {
  return key.startsWith(DRAFT_PREFIX) && key.includes(`:${userId}:`);
}

/**
 * Clear text/form drafts for an explicit logout. Unsent chat outbox entries
 * are intentionally excluded: a 401 must never erase a user's message.
 */
export async function clearDraftsForUser(userId: string | null): Promise<void> {
  if (!userId) {
    console.warn("Skipping local draft cleanup without a user id");
    return;
  }
  const keys = await AsyncStorage.getAllKeys();
  const ownedKeys = keys.filter(
    (key) => isOwnedDraftKey(key, userId) && !key.startsWith(`${DRAFT_PREFIX}outbox:`),
  );
  if (ownedKeys.length > 0) await AsyncStorage.multiRemove(ownedKeys);
}

/** Remove one user's unsent messages only during an explicit logout. */
export async function clearChatOutboxForUser(userId: string | null): Promise<void> {
  if (!userId) {
    console.warn("Skipping local outbox cleanup without a user id");
    return;
  }
  const keys = await AsyncStorage.getAllKeys();
  const ownedOutboxKeys = keys.filter(
    (key) =>
      key.startsWith(`${DRAFT_PREFIX}outbox:`) &&
      isOwnedDraftKey(key, userId),
  );
  if (ownedOutboxKeys.length > 0) await AsyncStorage.multiRemove(ownedOutboxKeys);
}

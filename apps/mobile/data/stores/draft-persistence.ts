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

/** Clear every workspace draft and unsent message owned by a user on logout/401. */
export async function clearDraftsForUser(userId: string | null): Promise<void> {
  const keys = await AsyncStorage.getAllKeys();
  const prefix = `${DRAFT_PREFIX}`;
  const ownedKeys = keys.filter(
    (key) =>
      key.startsWith(prefix) &&
      (userId === null || key.includes(`:${userId}:`)),
  );
  if (ownedKeys.length > 0) await AsyncStorage.multiRemove(ownedKeys);
}

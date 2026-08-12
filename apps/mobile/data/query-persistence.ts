/**
 * User-scoped offline query cache.
 *
 * The persister is attached only after authentication is resolved so the
 * storage key can include the user id. This prevents one account's cached
 * workspace, inbox, issue, or chat data from hydrating for another account.
 */
import AsyncStorage from "@react-native-async-storage/async-storage";
import { createAsyncStoragePersister } from "@tanstack/query-async-storage-persister";
import {
  persistQueryClientRestore,
  persistQueryClientSubscribe,
} from "@tanstack/react-query-persist-client";
import { queryClient } from "./query-client";

const CACHE_PREFIX = "multica_query_cache:";
const CACHE_MAX_AGE = 7 * 24 * 60 * 60 * 1000;
const CACHE_BUSTER = "offline-cache-v1";

let activeUserId: string | null = null;
let unsubscribe: (() => void) | null = null;

function storageKey(userId: string) {
  return `${CACHE_PREFIX}${userId}`;
}

function createPersister(userId: string) {
  return createAsyncStoragePersister({
    storage: AsyncStorage,
    key: storageKey(userId),
  });
}

const dehydrateOptions = {
  // Never retain an outbox. Failed/paused writes must be retried manually.
  shouldDehydrateMutation: () => false,
  // Only completed read data can be useful on an offline cold start.
  shouldDehydrateQuery: (query: { state: { status: string } }) =>
    query.state.status === "success",
};

/** Restore and begin persisting one account's successful read queries. */
export async function restoreQueryCacheForUser(userId: string): Promise<void> {
  if (activeUserId === userId) return;

  unsubscribe?.();
  unsubscribe = null;
  queryClient.clear();

  const persister = createPersister(userId);
  await persistQueryClientRestore({
    queryClient,
    persister,
    maxAge: CACHE_MAX_AGE,
    buster: CACHE_BUSTER,
  });
  activeUserId = userId;
  unsubscribe = persistQueryClientSubscribe({
    queryClient,
    persister,
    buster: CACHE_BUSTER,
    dehydrateOptions,
  });
}

/** Remove the in-memory and persisted query state for a signed-out user. */
export async function clearQueryCacheForUser(userId: string | null): Promise<void> {
  unsubscribe?.();
  unsubscribe = null;
  activeUserId = null;
  queryClient.clear();
  if (userId) await AsyncStorage.removeItem(storageKey(userId));
}

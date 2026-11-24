/**
 * The app's single writer for the auth token.
 *
 * Keyed identically to web/desktop ("multica_token") so logic stays aligned
 * with packages/core/auth/store.ts even though storage backends differ.
 *
 * Every Keychain operation is async, and several things race to write this one
 * value: logout, a 401 teardown, a new sign-in, and a background session
 * renewal that started before any of them. Interleaved, they produce states no
 * single path intends — most sharply, a renewal writing its result on top of a
 * sign-in that finished while it was suspended, leaving the app logged in as
 * one account and persisted as another.
 *
 * So writes are SERIALIZED here, and a conditional write can be decided inside
 * that serialization (see commitRenewedToken). That is what makes a stale
 * write refusable instead of something to undo afterwards — undoing it means
 * deleting whatever is there now, which may belong to a session that started
 * after the stale one and has every right to exist.
 */
import * as SecureStore from "expo-secure-store";

import { sessionEpochChanged } from "./session-epoch";

const TOKEN_KEY = "multica_token";

// Tail of the write queue. Each operation chains onto the previous one so no
// two can be in flight at once; failures do not stall the queue.
let writes: Promise<unknown> = Promise.resolve();

function serialize<T>(operation: () => Promise<T>): Promise<T> {
  const result = writes.then(operation, operation);
  writes = result.catch(() => undefined);
  return result;
}

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  await serialize(() => SecureStore.setItemAsync(TOKEN_KEY, token));
}

export async function clearToken(): Promise<void> {
  await serialize(() => SecureStore.deleteItemAsync(TOKEN_KEY));
}

/**
 * Persist a renewed token, but only if the session it renews is still the one
 * the app is using. Returns whether the write happened.
 *
 * The check and the write are one serialized unit, which is the whole point: a
 * logout or a sign-in cannot land between them, so "is this still current?"
 * cannot go stale between being asked and being acted on. Both conditions are
 * needed and neither implies the other — the epoch catches a teardown that has
 * STARTED but not finished (its Keychain delete is still in flight, so a read
 * would still return the old token), and the value comparison catches a
 * credential replaced by any path that did not move the epoch.
 */
export async function commitRenewedToken(
  token: string,
  expected: { epoch: number; previousToken: string },
): Promise<boolean> {
  return serialize(async () => {
    if (sessionEpochChanged(expected.epoch)) return false;
    const current = await SecureStore.getItemAsync(TOKEN_KEY);
    if (current !== expected.previousToken) return false;
    await SecureStore.setItemAsync(TOKEN_KEY, token);
    return true;
  });
}

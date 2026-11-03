import * as SecureStore from "expo-secure-store";
import { api } from "./api";
import { currentSessionEpoch, sessionEpochChanged } from "./session-epoch";

const PUSH_TOKEN_KEY = "multica_push_token";
let token: string | null = null;
let writes: Promise<unknown> = Promise.resolve();

function serialize<T>(operation: () => Promise<T>): Promise<T> {
  const result = writes.then(operation, operation);
  writes = result.catch(() => undefined);
  return result;
}

export function registerPushToken(value: string, epoch: number): Promise<void> {
  return serialize(async () => {
    if (sessionEpochChanged(epoch)) return;
    const previous = token ?? await SecureStore.getItemAsync(PUSH_TOKEN_KEY);
    if (sessionEpochChanged(epoch)) return;
    if (previous && previous !== value) {
      await api.updateMobilePush(previous, false);
    }
    if (sessionEpochChanged(epoch)) return;
    token = value;
    await SecureStore.setItemAsync(PUSH_TOKEN_KEY, value);
    if (sessionEpochChanged(epoch)) return;
    await api.updateMobilePush(value, true);
  });
}

// Logout waits for an in-flight registration before deleting, while the old
// credential still exists. A failed cleanup must not prevent local sign-out.
export function unregisterPushToken(): Promise<void> {
  const epoch = currentSessionEpoch();
  return serialize(async () => {
    if (sessionEpochChanged(epoch)) return;
    try {
      const previous = token ?? await SecureStore.getItemAsync(PUSH_TOKEN_KEY);
      if (sessionEpochChanged(epoch) || !previous) return;
      await api.updateMobilePush(previous, false);
      token = null;
      await SecureStore.deleteItemAsync(PUSH_TOKEN_KEY);
    } catch {
      console.warn("[push] subscription cleanup failed");
    }
  });
}

export function refreshPushRegistration(): Promise<void> {
  if (!token) return Promise.resolve();
  return registerPushToken(token, currentSessionEpoch()).catch(() => {
    console.warn("[push] subscription renewal deferred");
  });
}

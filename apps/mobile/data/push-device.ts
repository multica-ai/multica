import * as SecureStore from "expo-secure-store";

import { api } from "./api";

const PUSH_TOKEN_KEY = "multica_expo_push_token";

export async function savePushDevice(token: string): Promise<void> {
  await api.registerPushDevice(token);
  await SecureStore.setItemAsync(PUSH_TOKEN_KEY, token);
}

export async function unregisterPushDevice(): Promise<void> {
  const token = await SecureStore.getItemAsync(PUSH_TOKEN_KEY);
  if (!token) return;
  try {
    await api.unregisterPushDevice(token);
    await SecureStore.deleteItemAsync(PUSH_TOKEN_KEY);
  } catch (error) {
    // Keep the token so a later registration/logout can retry. Registering on
    // another account atomically transfers ownership server-side.
    console.warn("[push] failed to unregister device", error);
  }
}

/**
 * Thin wrapper around expo-secure-store for the auth token.
 * Keyed identically to web/desktop ("multica_token") so logic stays aligned
 * with packages/core/auth/store.ts even though storage backends differ.
 */
import * as SecureStore from "expo-secure-store";
import type { User } from "@multica/core/types";

const TOKEN_KEY = "multica_token";
const USER_KEY = "multica_user";

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  await SecureStore.setItemAsync(TOKEN_KEY, token);
}

export async function clearToken(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
}

/**
 * A last-known-good account snapshot lets an otherwise valid session enter
 * the app when a cold start cannot reach the API. It is not an auth decision:
 * the server's 401 response remains the only signal that invalidates a token.
 */
export async function getCachedUser(): Promise<User | null> {
  const raw = await SecureStore.getItemAsync(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as User;
  } catch {
    // Treat corrupted local state as absent instead of preventing startup.
    await SecureStore.deleteItemAsync(USER_KEY);
    return null;
  }
}

export async function setCachedUser(user: User): Promise<void> {
  await SecureStore.setItemAsync(USER_KEY, JSON.stringify(user));
}

export async function clearCachedUser(): Promise<void> {
  await SecureStore.deleteItemAsync(USER_KEY);
}

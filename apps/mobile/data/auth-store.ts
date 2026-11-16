/**
 * Mobile auth store — Zustand. Logic mirrors packages/core/auth/store.ts:
 *   - Token written ONLY on successful verifyCode
 *   - 401 → clear token; non-401 (5xx / network blip) → preserve token so
 *     the next launch can retry
 *   - logout = clear token + clear in-memory user + setToken(null)
 *
 * NOT shared with web/desktop (per Sharing Principles in root CLAUDE.md).
 * Storage backend is expo-secure-store (mobile only); web uses HttpOnly
 * cookies, desktop uses localStorage via StorageAdapter.
 */
import { create } from "zustand";
import type { User } from "@multica/core/types";
import { api, ApiError } from "./api";
import { clearToken, getToken, setToken } from "./secure-storage";
import { currentSessionEpoch, invalidateSessionEpoch, sessionEpochChanged } from "./session-epoch";
import { useWorkspaceStore } from "./workspace-store";
import { initializeServerConfig } from "./server-config-store";
import { stopSessionConnections } from "./realtime/session-connections";
import { clearNativeSessionCookies, initializeNativeSession } from "./native-session";

interface AuthState {
  user: User | null;
  isLoading: boolean;
  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  logout: () => Promise<void>;
  /** Overwrite the in-memory user — call after PATCH /api/me so name/avatar
   *  edits land without a refetch. Server response is the source of truth. */
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,

  initialize: async () => {
    const epoch = currentSessionEpoch();
    await initializeServerConfig();
    await initializeNativeSession();
    if (sessionEpochChanged(epoch)) return;
    // Restore the persisted workspace slug alongside the auth token so the
    // entry redirect (app/index.tsx) can route directly to the last-used
    // workspace without flashing /select-workspace.
    await useWorkspaceStore.getState().restoreSlug();

    const token = await getToken();
    if (sessionEpochChanged(epoch)) return;
    if (!token) {
      set({ isLoading: false });
      return;
    }
    api.setToken(token);
    try {
      const user = await api.getMe();
      if (sessionEpochChanged(epoch)) return;
      set({ user, isLoading: false });
    } catch (err) {
      if (sessionEpochChanged(epoch)) return;
      // Only clear token on a genuine 401. Network blips / 5xx keep the
      // token so the next launch (or a manual refresh) can retry.
      if (err instanceof ApiError && err.status === 401) {
        // Synchronous first, before the awaited delete: anything already
        // in flight has to learn the credential is dead now, not once the
        // Keychain write lands.
        invalidateSessionEpoch();
        await clearToken();
        api.setToken(null);
      }
      set({ user: null, isLoading: false });
    }
  },

  sendCode: async (email) => {
    await api.sendCode(email);
  },

  verifyCode: async (email, code) => {
    const startedAt = currentSessionEpoch();
    const { token, user } = await api.verifyCode(email, code);
    if (sessionEpochChanged(startedAt)) throw new Error("Server session changed");
    // Signing in replaces the credential just as decisively as signing out:
    // a renewal still in flight for the previous account must not write its
    // result over this one.
    invalidateSessionEpoch();
    const epoch = currentSessionEpoch();
    await setToken(token);
    if (sessionEpochChanged(epoch)) throw new Error("Server session changed");
    api.setToken(token);
    set({ user });
    return user;
  },

  logout: async () => {
    // First statement in the function, before any await. The Keychain delete
    // below is async, so until it lands a concurrent read still returns the
    // token being removed — the epoch is what makes this instant.
    invalidateSessionEpoch();
    api.resetSession();
    stopSessionConnections();
    set({ user: null, isLoading: false });
    await Promise.all([clearToken(), clearNativeSessionCookies()]);
  },

  setUser: (user) => set({ user }),
}));

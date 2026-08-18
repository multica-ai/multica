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
import {
  clearCachedUser,
  clearToken,
  getCachedUser,
  getToken,
  setCachedUser,
  setToken,
} from "./secure-storage";
import { useWorkspaceStore } from "./workspace-store";
import {
  clearQueryCacheForUser,
  restoreQueryCacheForUser,
} from "./query-persistence";
import {
  clearChatOutboxForUser,
  clearDraftsForUser,
} from "./stores/draft-persistence";
import { clearChatOutbox } from "./stores/chat-outbox-store";

interface AuthState {
  user: User | null;
  isLoading: boolean;
  /** Credential presence is distinct from an online account lookup. */
  hasToken: boolean;
  /** True when startup fell back to a local user snapshot after a non-401. */
  isOffline: boolean;
  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  logout: () => Promise<void>;
  /** Overwrite the in-memory user — call after PATCH /api/me so name/avatar
   *  edits land without a refetch. Server response is the source of truth. */
  setUser: (user: User) => void;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  isLoading: true,
  hasToken: false,
  isOffline: false,

  initialize: async () => {
    // Restore the persisted workspace slug alongside the auth token so the
    // entry redirect (app/index.tsx) can route directly to the last-used
    // workspace without flashing /select-workspace.
    await useWorkspaceStore.getState().restoreSlug();

    const token = await getToken();
    if (!token) {
      set({ hasToken: false, isLoading: false, isOffline: false });
      return;
    }
    api.setToken(token);
    const cachedUser = await getCachedUser();
    try {
      const user = await api.getMe();
      await setCachedUser(user);
      await restoreQueryCacheForUser(user.id);
      set({ user, hasToken: true, isLoading: false, isOffline: false });
    } catch (err) {
      // Only clear token on a genuine 401. Network blips / 5xx keep the
      // token so the next launch (or a manual refresh) can retry.
      if (err instanceof ApiError && err.status === 401) {
        await clearToken();
        await clearCachedUser();
        await clearQueryCacheForUser(cachedUser?.id ?? null);
        // A 401 invalidates server access, not the user's unsent work. Keep
        // drafts and outbox entries so signing in again can restore them.
        api.setToken(null);
        set({ user: null, hasToken: false, isLoading: false, isOffline: false });
        return;
      }
      if (cachedUser) {
        await restoreQueryCacheForUser(cachedUser.id);
      }
      // A non-401 must never turn a valid local credential into a logout.
      // An older install may have a token but no snapshot; the route renders
      // an explicit offline account-info screen rather than /login.
      set({
        user: cachedUser,
        hasToken: true,
        isLoading: false,
        isOffline: true,
      });
    }
  },

  sendCode: async (email) => {
    await api.sendCode(email);
  },

  verifyCode: async (email, code) => {
    const { token, user } = await api.verifyCode(email, code);
    await setToken(token);
    await setCachedUser(user);
    api.setToken(token);
    await restoreQueryCacheForUser(user.id);
    set({ user, hasToken: true, isOffline: false });
    return user;
  },

  logout: async () => {
    const userId = get().user?.id ?? (await getCachedUser())?.id ?? null;
    await clearToken();
    await clearCachedUser();
    await clearQueryCacheForUser(userId);
    await clearDraftsForUser(userId);
    await clearChatOutboxForUser(userId);
    clearChatOutbox();
    api.setToken(null);
    set({ user: null, hasToken: false, isOffline: false });
  },

  setUser: (user) => {
    void setCachedUser(user);
    set({ user });
  },
}));

/**
 * Mobile auth store — Zustand. Logic mirrors packages/core/auth/store.ts:
 *   - Token written ONLY on successful verifyCode
 *   - 401 → clear session; non-401 (5xx / network blip) → preserve the
 *     credential so the next launch can retry
 *   - logout = clear session + clear in-memory user + setToken(null)
 *
 * NOT shared with web/desktop (per Sharing Principles in root CLAUDE.md).
 * Storage backend is expo-secure-store (mobile only); web uses HttpOnly
 * cookies, desktop uses localStorage via StorageAdapter.
 */
import { create } from "zustand";
import type { User } from "@multica/core/types";
import { api, ApiError } from "./api";
import {
  clearSession,
  getSession,
  saveSession,
  saveSessionUser,
} from "./secure-storage";
import { useWorkspaceStore } from "./workspace-store";

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

export const useAuthStore = create<AuthState>((set) => {
  /**
   * Initialization is single-flight. Overlapping runs used to race on the
   * final `set`: a 401 run could clear the session, then a slower non-401 run
   * would write `hasToken: true` and a user back into the store on top of a
   * credential the server had already invalidated. Concurrent callers now
   * share one run, so the last write is always that run's own conclusion.
   */
  let inFlight: Promise<void> | null = null;

  const runInitialize = async () => {
    // isLoading was previously only ever set to false, which left the
    // /offline Retry button permanently enabled and its spinner unreachable.
    set({ isLoading: true });
    try {
      // Restore the persisted workspace slug alongside the auth token so the
      // entry redirect (app/index.tsx) can route directly to the last-used
      // workspace without flashing /select-workspace.
      await useWorkspaceStore.getState().restoreSlug();

      const session = await getSession();
      if (!session) {
        api.setToken(null);
        set({ user: null, hasToken: false, isOffline: false });
        return;
      }
      api.setToken(session.token);
      try {
        const user = await api.getMe();
        await saveSession(session.token, user);
        set({ user, hasToken: true, isOffline: false });
      } catch (err) {
        // Only clear on a genuine 401. Network blips / 5xx keep the
        // credential so the next launch (or a manual refresh) can retry.
        if (err instanceof ApiError && err.status === 401) {
          await clearSession();
          api.setToken(null);
          set({ user: null, hasToken: false, isOffline: false });
          return;
        }
        // A non-401 must never turn a valid local credential into a logout.
        // An older install may have a credential but no snapshot; the route
        // renders an explicit offline account-info screen rather than /login.
        set({ user: session.user, hasToken: true, isOffline: true });
      }
    } finally {
      // Landed in one place so an unexpected throw cannot strand the app on
      // a permanent spinner now that isLoading starts true.
      set({ isLoading: false });
    }
  };

  return {
    user: null,
    isLoading: true,
    hasToken: false,
    isOffline: false,

    initialize: () => {
      inFlight ??= runInitialize().finally(() => {
        inFlight = null;
      });
      return inFlight;
    },

    sendCode: async (email) => {
      await api.sendCode(email);
    },

    verifyCode: async (email, code) => {
      const { token, user } = await api.verifyCode(email, code);
      // One write: there is no instant at which this device holds the new
      // credential next to the previous account's snapshot.
      await saveSession(token, user);
      api.setToken(token);
      set({ user, hasToken: true, isOffline: false });
      return user;
    },

    logout: async () => {
      await clearSession();
      api.setToken(null);
      set({ user: null, hasToken: false, isOffline: false });
    },

    setUser: (user) => {
      set({ user });
      // Persisting the snapshot is best-effort and must not block the UI
      // edit, but a rejected write still has to be swallowed explicitly —
      // a bare `void` here surfaced as an unhandled rejection.
      void saveSessionUser(user).catch(() => {});
    },
  };
});

// TECH-3413 — Classic vs Dynamic inbox mode.
//
// The dynamic inbox is gated by the `cerebro_inbox_dynamic` feature flag
// (availability) AND a per-user preference (the user's own choice). This module
// owns only the per-user choice so it can live in the low-level cerebro-inbox
// package and be mounted from the classic inbox (upstream) without a dependency
// cycle. The flag check lives in the switcher / toggle button.
//
// The preference rides the server-synced `user.preferences` JSON blob
// (PATCH /api/me/preferences), so the choice follows the user across devices.
import { useCallback } from "react";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";

export type InboxMode = "classic" | "dynamic";

export const INBOX_MODE_KEY = "cerebro_inbox_mode";
export const DEFAULT_INBOX_MODE: InboxMode = "classic";

export function toInboxMode(value: unknown): InboxMode {
  return value === "dynamic" ? "dynamic" : DEFAULT_INBOX_MODE;
}

/** The user's chosen inbox mode (server-synced preference). Default: classic. */
export function useInboxMode(): InboxMode {
  const user = useAuthStore((s) => s.user);
  return toInboxMode(user?.preferences?.[INBOX_MODE_KEY]);
}

/** Persist the inbox mode to the server-synced preference blob. */
export function useSetInboxMode(): (mode: InboxMode) => Promise<void> {
  const setUser = useAuthStore((s) => s.setUser);
  return useCallback(
    async (mode: InboxMode) => {
      const updated = await api.updateMyPreferences({ [INBOX_MODE_KEY]: mode });
      setUser(updated);
    },
    [setUser],
  );
}

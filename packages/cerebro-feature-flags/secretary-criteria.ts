// TECH-3557 follow-up — user-configurable criteria for what the inbox Secretary
// block is allowed to pick from. Persisted in the server-synced user.preferences
// blob (like the inbox layout / inbox mode), so the choice follows the user
// across devices. The settings UI lives in this package (rendered under the
// Secretary toggle in the Cerebro features tab); the actual entry-matching lives
// in cerebro-inbox-dynamic, which knows the inbox entry shape.
"use client";

import { useCallback } from "react";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";

export const SECRETARY_CRITERIA_KEY = "cerebro_secretary_criteria";

export interface SecretaryCriteria {
  /** Only pick rows that are still unread. */
  unreadOnly: boolean;
  /** Include issue notifications (mentions, comments, reminders, …). */
  includeIssues: boolean;
  /** Include channel / DM rows. */
  includeChannels: boolean;
  /** Include agent-chat rows. */
  includeChats: boolean;
  /**
   * TECH-3557 follow-up (Jesper) — start the Secretary block collapsed so it
   * takes minimal space, and only unfold when you want to start a batch. When
   * false the setup controls are shown immediately.
   */
  startFolded: boolean;
}

export const DEFAULT_SECRETARY_CRITERIA: SecretaryCriteria = {
  unreadOnly: false,
  includeIssues: true,
  includeChannels: true,
  includeChats: true,
  startFolded: true,
};

/** Defensive: read an untrusted criteria blob from preferences. Missing or
 *  malformed fields fall back to the default so an old/garbage blob never
 *  hides every row. */
export function readSecretaryCriteria(value: unknown): SecretaryCriteria {
  if (!value || typeof value !== "object") return DEFAULT_SECRETARY_CRITERIA;
  const v = value as Partial<Record<keyof SecretaryCriteria, unknown>>;
  const bool = (key: keyof SecretaryCriteria) =>
    typeof v[key] === "boolean" ? (v[key] as boolean) : DEFAULT_SECRETARY_CRITERIA[key];
  return {
    unreadOnly: bool("unreadOnly"),
    includeIssues: bool("includeIssues"),
    includeChannels: bool("includeChannels"),
    includeChats: bool("includeChats"),
    startFolded: bool("startFolded"),
  };
}

/** The user's Secretary selection criteria (server-synced preference). */
export function useSecretaryCriteria(): SecretaryCriteria {
  const user = useAuthStore((s) => s.user);
  return readSecretaryCriteria(user?.preferences?.[SECRETARY_CRITERIA_KEY]);
}

/** Persist a patch to the Secretary criteria preference. Reads the freshest
 *  value from the store at call time so rapid toggles don't clobber each other. */
export function useSetSecretaryCriteria(): (patch: Partial<SecretaryCriteria>) => Promise<void> {
  const setUser = useAuthStore((s) => s.setUser);
  return useCallback(
    async (patch) => {
      const current = readSecretaryCriteria(
        useAuthStore.getState().user?.preferences?.[SECRETARY_CRITERIA_KEY],
      );
      const next: SecretaryCriteria = { ...current, ...patch };
      const updated = await api.updateMyPreferences({ [SECRETARY_CRITERIA_KEY]: next });
      setUser(updated);
    },
    [setUser],
  );
}

"use client";

import { useEffect, useState } from "react";
import { useFeatureFlag, useFlagValue } from "@multica/cerebro-feature-flags";

const STORAGE_PREFIX = "cerebro_firtal_welcome_seen:";

function storageKey(userId: string): string {
  return `${STORAGE_PREFIX}${userId}`;
}

/**
 * Reads the cerebro_firtal_welcome flag for the current workspace.
 *
 * Uses `useFeatureFlag` (not `useFlagValue`) so the workspace's server-side
 * overrides hydrate the local store on first use. Falls back to
 * `useFlagValue` when no workspace context is active (e.g. on the global
 * /onboarding route) — in that case only the default value is consulted.
 */
export function useFirtalWelcomeEnabled(): boolean {
  // `useFeatureFlag` throws when there's no workspace context; the
  // global-route caller wraps with try/catch via a flag-aware variant.
  return useFlagValue("cerebro_firtal_welcome");
}

/**
 * Workspace-scoped variant — call inside WorkspaceSlugProvider so the
 * server-side overrides for this workspace hydrate the flag store.
 */
export function useFirtalWelcomeEnabledForWorkspace(): boolean {
  return useFeatureFlag("cerebro_firtal_welcome");
}

/**
 * Has this user been shown the Firtal welcome before? Tracked in
 * localStorage per user id — server-side persistence is out of scope for
 * v1 (the page is a one-time guide, missing the suppression on a new
 * browser is a non-issue).
 */
export function hasSeenFirtalWelcome(userId: string | null | undefined): boolean {
  if (!userId) return false;
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(storageKey(userId)) === "1";
  } catch {
    return false;
  }
}

export function markFirtalWelcomeSeen(userId: string | null | undefined): void {
  if (!userId) return;
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(storageKey(userId), "1");
  } catch {
    // localStorage may be unavailable (Safari private mode) — silently skip;
    // the user just sees the welcome again next session.
  }
}

/**
 * Reactive variant of hasSeenFirtalWelcome. Returns true only on the client
 * after the localStorage read settles, so server rendering never reports a
 * "seen" state that the hydration pass would have to retract.
 */
export function useShouldShowFirtalWelcome(userId: string | null | undefined): boolean {
  const enabled = useFirtalWelcomeEnabled();
  const [seen, setSeen] = useState<boolean | null>(null);
  useEffect(() => {
    setSeen(hasSeenFirtalWelcome(userId));
  }, [userId]);
  if (!enabled) return false;
  if (!userId) return false;
  if (seen === null) return false;
  return !seen;
}

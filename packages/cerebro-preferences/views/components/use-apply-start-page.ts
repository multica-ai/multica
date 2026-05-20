"use client";

import { useEffect, useRef } from "react";
import { useAuthStore } from "@multica/core/auth";
import {
  START_PAGE_DESKTOP_KEY,
  START_PAGE_MOBILE_KEY,
  VALID_START_PAGES,
} from "./start-page-preference-section";

export type StartPagePlatform = "desktop" | "mobile";

function readStartPage(
  preferences: Record<string, unknown> | undefined | null,
  platform: StartPagePlatform,
): string | null {
  const key =
    platform === "desktop" ? START_PAGE_DESKTOP_KEY : START_PAGE_MOBILE_KEY;
  const value = preferences?.[key];
  if (typeof value === "string" && VALID_START_PAGES.has(value)) return value;
  return null;
}

/**
 * Resolve the user's preferred start page string ("issues", "inbox", ...)
 * or null if no preference is set / the stored value is invalid.
 */
export function getPreferredStartPage(
  user: { preferences?: Record<string, unknown> | null } | null | undefined,
  platform: StartPagePlatform,
): string | null {
  if (!user) return null;
  return readStartPage(user.preferences, platform);
}

/**
 * Apply the user's preferred start page exactly once per authenticated
 * session. Fires for both fresh OAuth login (user transitions null → set)
 * and app restart with a persisted session (user is set on hydration).
 *
 * The `ready` flag gates the effect until the caller has everything it
 * needs to navigate (e.g. workspace list fetched). The "applied" latch
 * resets on logout so the next login picks up the preference again.
 */
export function useApplyStartPage(opts: {
  platform: StartPagePlatform;
  ready: boolean;
  navigate: (page: string) => void;
}) {
  const user = useAuthStore((s) => s.user);
  const applied = useRef(false);

  useEffect(() => {
    if (!user) applied.current = false;
  }, [user]);

  useEffect(() => {
    if (applied.current) return;
    if (!user || !opts.ready) return;
    const page = getPreferredStartPage(user, opts.platform);
    applied.current = true;
    if (page) opts.navigate(page);
  }, [user, opts.ready, opts.platform, opts.navigate, opts]);
}

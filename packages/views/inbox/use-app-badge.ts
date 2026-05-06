"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { appBadgeUnreadCountOptions } from "@multica/core/inbox/queries";
import { useAuthStore } from "@multica/core/auth";

// Badging API: iOS 16.4+ Safari (installed PWA), Android Chrome, desktop
// Chrome. Firefox/older Safari simply lack the methods. Cast through a
// narrow shape so we don't pollute the global Navigator type.
type BadgingNavigator = {
  setAppBadge?: (count?: number) => Promise<void>;
  clearAppBadge?: () => Promise<void>;
};

function applyAppBadge(count: number) {
  if (typeof navigator === "undefined") return;
  const nav = navigator as BadgingNavigator;
  if (count <= 0) {
    nav.clearAppBadge?.().catch(() => undefined);
    return;
  }
  nav.setAppBadge?.(count).catch(() => undefined);
}

// Tells the active service worker to update the OS app badge. Useful for the
// installed PWA case on iOS where setAppBadge from a normal page isn't
// always honored unless the SW does it. Best-effort — silently no-ops when
// the SW isn't ready or the API isn't supported.
function postBadgeToSw(count: number) {
  if (typeof navigator === "undefined") return;
  const sw = navigator.serviceWorker;
  if (!sw) return;
  sw.ready
    .then((reg) => reg.active?.postMessage({ type: "set-app-badge", count }))
    .catch(() => undefined);
}

/**
 * Keeps the OS app-icon badge in sync with the user's total unread inbox
 * count across every workspace they belong to. Mounted once at the
 * authenticated layout level — the underlying query is cross-workspace.
 *
 * The service worker also sets the badge when a Web Push arrives; this hook
 * covers the cases where no push fires (in-app reads/archives, returning
 * to a tab that missed events while offline, login, logout).
 */
export function useAppBadgeSync() {
  const isAuthenticated = useAuthStore((s) => !!s.user);

  const { data } = useQuery({
    ...appBadgeUnreadCountOptions(),
    enabled: isAuthenticated,
  });

  useEffect(() => {
    // On sign-out, force the badge to zero so a stale count doesn't linger
    // on the home screen for the next user of this device.
    if (!isAuthenticated) {
      applyAppBadge(0);
      postBadgeToSw(0);
      return;
    }
    if (data === undefined) return;
    applyAppBadge(data.count);
    postBadgeToSw(data.count);
  }, [isAuthenticated, data]);
}

"use client";

import { useEffect, useRef } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  extractWorkspaceSlug,
  getActiveTab,
  useTabStore,
} from "@multica/core/tabs";
import { useSessionTabsEnabled } from "@multica/views/layout/session-tabs";

function locationUrl(pathname: string, search: string, hash = ""): string {
  return `${pathname}${search}${hash}`;
}

function windowUrl(pathname: string, search: string): string {
  return locationUrl(
    pathname,
    search ? `?${search}` : "",
    window.location.hash,
  );
}

/**
 * Align the Next App Router with the active session, and seed the session
 * store from the address bar on first load. Compact viewports skip the
 * store → URL half so the browser remains the only navigator.
 */
export function WebTabCoordinator() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const sessionTabs = useSessionTabsEnabled();
  const search = searchParams.toString();
  const seeded = useRef(false);

  useEffect(() => {
    const slug = extractWorkspaceSlug(pathname);
    if (!slug) return;
    if (seeded.current) return;
    seeded.current = true;
    useTabStore.getState().switchWorkspace(slug, windowUrl(pathname, search));
  }, [pathname, search]);

  useEffect(() => {
    if (!sessionTabs) {
      const slug = extractWorkspaceSlug(pathname);
      if (!slug) return;
      const store = useTabStore.getState();
      const active = getActiveTab(store);
      const current = windowUrl(pathname, search);
      if (active?.url !== current) {
        store.switchWorkspace(slug, current);
      }
      return;
    }

    return useTabStore.subscribe(() => {
      const active = getActiveTab(useTabStore.getState());
      if (!active) return;
      const current = locationUrl(
        window.location.pathname,
        window.location.search,
        window.location.hash,
      );
      if (active.url === current) return;
      router.replace(active.url);
    });
  }, [sessionTabs, pathname, search, router]);

  return null;
}

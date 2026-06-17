"use client";

import { Suspense } from "react";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";

function NavigationProviderInner({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const adapter: NavigationAdapter = {
    push: router.push,
    replace: router.replace,
    // FIR-2684: change the URL without a Next.js RSC route navigation. Calling
    // router.push/replace for a pure query-param change (e.g. inbox ?issue=)
    // triggers a server RSC payload fetch + route re-render — the "whole page
    // reloads" symptom. The native History API updates the URL silently; Next
    // syncs usePathname/useSearchParams from it (supported since 14.1) with no
    // round-trip and no re-render.
    replaceSilent: (path: string) => {
      window.history.replaceState(null, "", path);
    },
    back: router.back,
    pathname,
    searchParams: new URLSearchParams(searchParams.toString()),
    // TECH-3702: web has no app-tab model, so "open in new tab" maps to a real
    // browser tab. Without this, modifier-click handlers that call
    // e.preventDefault() and then `if (openInNewTab) openInNewTab(...)` (issue
    // and project mentions, skill chips, AppLink, avatars) silently no-op on
    // web — the native anchor is suppressed and nothing replaces it, so Cmd+click
    // on an issue link does nothing despite the "open in new tab" preference.
    openInNewTab: (path: string) => {
      window.open(path, "_blank", "noopener,noreferrer");
    },
    getShareableUrl: (path: string) =>
      typeof window === "undefined" ? path : window.location.origin + path,
    // router.prefetch is a no-op in dev mode by Next.js design; in production
    // it warms the RSC payload + route chunk so the next push() commits with
    // no network round-trip. Safe to call repeatedly — Next dedupes internally.
    prefetch: (path: string) => {
      router.prefetch(path);
    },
  };

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

export function WebNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Suspense>
      <NavigationProviderInner>{children}</NavigationProviderInner>
    </Suspense>
  );
}

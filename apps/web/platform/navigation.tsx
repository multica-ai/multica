"use client";

import { Suspense, useEffect } from "react";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";

/**
 * Web half of the `multica:navigate` bridge — the event shared content
 * (comments, chat, issue descriptions) fires when a link resolves to an in-app
 * destination. Desktop's shell answers it by opening a tab; on the web the
 * equivalent is a router push in place. Without this the event has no listener
 * and such links do nothing at all.
 */
function useInternalLinkHandler(router: ReturnType<typeof useRouter>) {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent<{ path?: string }>).detail?.path;
      if (!path) return;
      router.push(path);
    };
    window.addEventListener("multica:navigate", handler);
    return () => window.removeEventListener("multica:navigate", handler);
  }, [router]);
}

function NavigationProviderInner({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  useInternalLinkHandler(router);

  const adapter: NavigationAdapter = {
    push: router.push,
    replace: router.replace,
    back: router.back,
    pathname,
    searchParams: new URLSearchParams(searchParams.toString()),
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

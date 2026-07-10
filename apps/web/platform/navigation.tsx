"use client";

import { Suspense, useMemo } from "react";
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
  const serializedSearchParams = searchParams.toString();

  const adapter = useMemo<NavigationAdapter>(
    () => ({
      push: router.push,
      replace: router.replace,
      back: router.back,
      pathname,
      searchParams: new URLSearchParams(serializedSearchParams),
      getShareableUrl: (path: string) =>
        typeof window === "undefined" ? path : window.location.origin + path,
      // router.prefetch is a no-op in dev mode by Next.js design; in production
      // it warms the RSC payload + route chunk so the next push() commits with
      // no network round-trip. Safe to call repeatedly — Next dedupes internally.
      prefetch: (path: string) => {
        router.prefetch(path);
      },
    }),
    [pathname, router, serializedSearchParams],
  );

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

"use client";

import { Suspense } from "react";
import { useRouter, usePathname } from "@/i18n/navigation";
import { useSearchParams } from "next/navigation";
import { useLocale } from "next-intl";
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
  const locale = useLocale();

  const adapter: NavigationAdapter = {
    push: (path: string) => router.push(path),
    replace: (path: string) => router.replace(path),
    back: router.back.bind(router),
    pathname,
    searchParams: new URLSearchParams(searchParams.toString()),
    buildHref: (path: string) => `/${locale}${path}`,
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

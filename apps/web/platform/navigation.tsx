"use client";

import { Suspense, useEffect, useMemo, useSyncExternalStore } from "react";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import {
  NavigationProvider,
  type LinkClickIntent,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { useSessionTabsEnabled } from "@multica/views/layout/session-tabs";
import {
  extractWorkspaceSlug,
  getActiveTab,
  navigateSessionPush,
  navigateSessionReplace,
  openSessionTab,
  routeContentLinkPath,
  splitTabUrl,
  useActiveTabUrl,
  useTabStore,
} from "@multica/core/tabs";
import { canGoBackInApp } from "./in-app-history";
import { WebTabCoordinator } from "./tab-coordinator";

function isTransitionPath(path: string): boolean {
  return extractWorkspaceSlug(path) === null;
}

/**
 * Web half of the `multica:navigate` bridge. Wide viewports route through
 * session tabs. Compact viewports keep today's Next/browser behaviour.
 */
function useInternalLinkHandler(
  router: ReturnType<typeof useRouter>,
  sessionTabs: boolean,
) {
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (
        e as CustomEvent<{ path?: string; disposition?: LinkClickIntent }>
      ).detail;
      const path = detail?.path;
      if (!path) return;
      if (sessionTabs && !isTransitionPath(path)) {
        routeContentLinkPath(path, detail?.disposition ?? "push");
        return;
      }
      if (
        detail?.disposition === "background-tab" ||
        detail?.disposition === "foreground-tab"
      ) {
        window.open(
          window.location.origin + path,
          "_blank",
          "noopener,noreferrer",
        );
        return;
      }
      router.push(path);
    };
    window.addEventListener("multica:navigate", handler);
    return () => window.removeEventListener("multica:navigate", handler);
  }, [router, sessionTabs]);
}

/**
 * The fragment is client-only state Next.js never surfaces: `usePathname()`
 * drops it, and a `router.replace("/x#y")` mutates `window.location` without
 * a render of its own. Reading it through an external store re-reads the URL
 * on every render and re-renders on the events that change it behind React's
 * back, so `adapter.hash` is never a stale copy.
 */
function subscribeToHash(onStoreChange: () => void): () => void {
  window.addEventListener("hashchange", onStoreChange);
  window.addEventListener("popstate", onStoreChange);
  return () => {
    window.removeEventListener("hashchange", onStoreChange);
    window.removeEventListener("popstate", onStoreChange);
  };
}

function NavigationProviderInner({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const sessionTabs = useSessionTabsEnabled();
  const activeUrl = useActiveTabUrl();
  const windowHash = useSyncExternalStore(
    subscribeToHash,
    () => window.location.hash,
    () => "",
  );
  useInternalLinkHandler(router, sessionTabs);

  const location = useMemo(() => {
    if (sessionTabs && activeUrl) {
      const { pathname: sessionPath, suffix } = splitTabUrl(activeUrl);
      const hashIdx = suffix.indexOf("#");
      const search = hashIdx === -1 ? suffix : suffix.slice(0, hashIdx);
      const hash = hashIdx === -1 ? "" : suffix.slice(hashIdx);
      return { pathname: sessionPath, search, hash };
    }
    return {
      pathname,
      search: `?${searchParams.toString()}`.replace(/^\?$/, ""),
      hash: windowHash,
    };
  }, [sessionTabs, activeUrl, pathname, searchParams, windowHash]);

  const adapter: NavigationAdapter = useMemo(() => {
    const nextAdapter: NavigationAdapter = {
      push: (path: string) => {
        if (!sessionTabs || isTransitionPath(path)) {
          router.push(path);
          return;
        }
        navigateSessionPush(path);
      },
      replace: (path: string) => {
        if (!sessionTabs || isTransitionPath(path)) {
          router.replace(path);
          return;
        }
        navigateSessionReplace(path);
      },
      back: () => {
        if (sessionTabs) {
          useTabStore.getState().goBack();
          return;
        }
        router.back();
      },
      forward: () => {
        if (sessionTabs) {
          useTabStore.getState().goForward();
          return;
        }
        router.forward();
      },
      canGoBack: sessionTabs
        ? () => {
            const active = getActiveTab(useTabStore.getState());
            return (active?.history.index ?? 0) > 0;
          }
        : canGoBackInApp,
      pathname: location.pathname,
      searchParams: new URLSearchParams(
        location.search.startsWith("?")
          ? location.search.slice(1)
          : location.search,
      ),
      hash: location.hash,
      getShareableUrl: (path: string) =>
        typeof window === "undefined" ? path : window.location.origin + path,
      prefetch: (path: string) => {
        router.prefetch(path);
      },
    };
    if (sessionTabs) {
      nextAdapter.openInNewTab = (path, title, opts) => {
        if (isTransitionPath(path)) {
          window.open(
            window.location.origin + path,
            "_blank",
            "noopener,noreferrer",
          );
          return;
        }
        openSessionTab(path, title, opts);
      };
    }
    return nextAdapter;
  }, [router, sessionTabs, location]);

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

export function WebNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <Suspense>
      <WebTabCoordinator />
      <NavigationProviderInner>{children}</NavigationProviderInner>
    </Suspense>
  );
}

import { useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { parseIssueWindowPath } from "../../../shared/issue-window";

/**
 * Answer the `multica:navigate` event inside a dedicated issue window (MUL-5208).
 *
 * The event is what a link in content (comment, description) fires once it
 * resolves to an in-app destination, including an absolute URL on this
 * deployment's own origin. Only the main shell listened for it, so in this
 * window such a click did nothing at all.
 *
 * Another issue opens in place — the same thing a mention chip does here, since
 * the window's adapter push is `navigateToIssue`. Any other app page cannot be
 * hosted by this single-route window, so it goes to the browser rather than
 * being swallowed.
 */
function useContentLinkHandler(
  navigate: ReturnType<typeof useNavigate>,
  runtimeConfig: typeof window.desktopAPI.runtimeConfig,
) {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent<{ path?: string }>).detail?.path;
      if (!path) return;
      const issuePath = parseIssueWindowPath(path);
      if (issuePath) {
        void navigate(issuePath.path);
        return;
      }
      if (!runtimeConfig.ok) return;
      void window.desktopAPI.openExternal(
        `${runtimeConfig.config.appUrl}${path}`,
      );
    };
    window.addEventListener("multica:navigate", handler);
    return () => window.removeEventListener("multica:navigate", handler);
  }, [navigate, runtimeConfig]);
}

/**
 * Navigation bridge for a dedicated issue window. Unlike the main Desktop
 * shell, this window owns a tiny MemoryRouter and intentionally accepts only
 * issue-detail routes. Keeping the bridge in the platform layer preserves the
 * MUL-4741 boundary around direct router navigation.
 */
export function IssueWindowNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const workspace = useCurrentWorkspace();
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  const currentPath = `${location.pathname}${location.search}${location.hash}`;

  useEffect(() => {
    window.desktopAPI.setRendererRouteContext({
      surface: "tab",
      path: currentPath,
      ...(workspace?.slug ? { workspaceSlug: workspace.slug } : {}),
    });
  }, [currentPath, workspace?.slug]);

  useContentLinkHandler(navigate, runtimeConfig);

  const adapter = useMemo<NavigationAdapter>(() => {
    const navigateToIssue = (path: string, replace = false) => {
      const issuePath = parseIssueWindowPath(path);
      if (!issuePath) return;
      void navigate(issuePath.path, { replace });
    };

    return {
      push: (path) => navigateToIssue(path),
      replace: (path) => navigateToIssue(path, true),
      back: () => void navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path, title) => {
        void window.desktopAPI.openIssueWindow({
          path,
          title: title ?? "Issue",
        });
      },
      getShareableUrl: (path) =>
        runtimeConfig.ok ? `${runtimeConfig.config.appUrl}${path}` : path,
    };
  }, [location.pathname, location.search, navigate, runtimeConfig]);

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

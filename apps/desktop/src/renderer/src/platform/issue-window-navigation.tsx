import { useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { parseIssueWindowPath } from "../../../shared/issue-window";

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

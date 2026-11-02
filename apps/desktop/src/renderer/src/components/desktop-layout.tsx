import { useEffect, useRef, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import {
  useNavigationInputBindings,
  useTabHistory,
} from "@/hooks/use-tab-history";
import {
  SidebarProvider,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "@multica/views/modals/registry";
import {
  AppRail,
  AppShellProvider,
  CompactNavSheet,
  GlobalShortcuts,
  NavigationProgress,
  NewIssueButton,
  SearchButton,
  ShellRouteDataPrefetch,
  SpacePanel,
} from "@multica/views/layout";
import { SearchCommand } from "@multica/views/search";
import { FloatingChat } from "@multica/views/chat";
import { WorkspaceSlugProvider, paths, useCurrentWorkspace } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace";
import {
  useNavigation,
  type LinkClickIntent,
} from "@multica/views/navigation";
import { getCurrentSlug, subscribeToCurrentSlug } from "@multica/core/platform";
import { useDesktopUnreadBadge } from "@multica/views/platform";
import { useT } from "@multica/views/i18n";
import {
  DesktopNavigationProvider,
  routeContentLinkPath,
} from "@/platform/navigation";
import { useRenderedRouteStore } from "@/stores/rendered-route-store";
import { TabBar } from "./tab-bar";
import { TabContent } from "./tab-content";
import { WindowOverlay } from "./window-overlay";
import { WindowToolbar, WINDOW_TOOLBAR_CLEARANCE } from "./window-toolbar";

const TOP_BAR_HEIGHT_CLASS = "h-10";
const RAIL_WIDTH = 64;
const TITLE_BAR_ACTION_CLASS =
  "flex size-7 items-center justify-center rounded-md text-faint-foreground transition-colors duration-150 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 [&_svg]:size-4";

function RailTopSpacer() {
  return <div className={cn("shrink-0", TOP_BAR_HEIGHT_CLASS)} />;
}

function useNativeNavigationGestures() {
  const { goBack, goForward } = useTabHistory();

  useEffect(() => {
    return window.desktopAPI.onNavigationGesture((gesture) => {
      if (gesture === "back") {
        goBack();
      } else {
        goForward();
      }
    });
  }, [goBack, goForward]);
}


// The main area's top bar doubles as a window drag region. The window
// toolbar (traffic lights, history, panel toggle) floats over the rail and
// the start of this bar, so the tabs start past whatever the rail leaves of
// its width. New issue and search hold the far end; the tabs give way to
// them.
function MainTopBar({ railMounted }: { railMounted: boolean }) {
  const { isCompact } = useSidebar();
  const toolbarClearance =
    railMounted && !isCompact
      ? WINDOW_TOOLBAR_CLEARANCE - RAIL_WIDTH
      : WINDOW_TOOLBAR_CLEARANCE;

  return (
    <header
      data-slot="main-top-bar"
      className={cn("relative shrink-0 flex items-center gap-2", TOP_BAR_HEIGHT_CLASS)}
      style={{ paddingLeft: toolbarClearance }}
    >
      <div
        aria-hidden
        className="absolute inset-y-0 right-0"
        style={
          {
            left: toolbarClearance,
            WebkitAppRegion: "drag",
          } as React.CSSProperties
        }
      />
      <div
        data-slot="main-top-bar-content"
        className="relative z-10 flex h-full min-w-0 max-w-full items-center"
      >
        <TabBar />
      </div>
      {railMounted && (
        <div
          data-slot="title-bar-actions"
          className="relative z-10 ml-auto flex shrink-0 items-center gap-2 pr-3"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        >
          <NewIssueButton className={TITLE_BAR_ACTION_CLASS} tooltipSide="bottom" />
          <SearchButton className={TITLE_BAR_ACTION_CLASS} tooltipSide="bottom" />
        </div>
      )}
    </header>
  );
}

// The content card: the space panel and the tab content share one surface,
// split by a single hairline. It hugs the rail; without one (compact, or no
// workspace yet) it mirrors the fixed mr-2 so it sits centred in the frame.
function MainCanvas({
  children,
  railMounted,
  showWorkspaceLoading,
}: {
  children: React.ReactNode;
  railMounted: boolean;
  showWorkspaceLoading: boolean;
}) {
  const { isCompact } = useSidebar();
  const { t } = useT("layout");
  const loadingLabel = t(($) => $.workspace_loader.loading_workspace);

  return (
    <div
      data-slot="main-canvas"
      className={cn(
        "relative mr-2 mb-2 flex min-h-0 flex-1 flex-row overflow-hidden rounded-xl bg-page-canvas shadow-[var(--surface-shadow)] ring-1 ring-surface-border",
        railMounted && !isCompact ? "ml-0" : "ml-2",
      )}
    >
      {railMounted && <SpacePanel />}
      <div className="relative flex min-w-0 flex-1 flex-col overflow-hidden">
        {children}
      </div>
      {showWorkspaceLoading && (
        <div
          aria-label={loadingLabel}
          aria-live="polite"
          className="absolute inset-0 z-20 flex items-center justify-center bg-page-canvas"
          role="status"
        >
          <div className="flex flex-col items-center gap-4">
            <MulticaIcon className="size-8 animate-pulse" />
            <p className="text-body text-muted-foreground">{loadingLabel}</p>
          </div>
        </div>
      )}
    </div>
  );
}

function useInternalLinkHandler() {
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (
        e as CustomEvent<{ path?: string; disposition?: LinkClickIntent }>
      ).detail;
      if (!detail?.path) return;
      routeContentLinkPath(detail.path, detail.disposition);
    };
    window.addEventListener("multica:navigate", handler);
    return () => window.removeEventListener("multica:navigate", handler);
  }, []);
}

/**
 * Bridge between the renderer and the Electron main process for inbox-level
 * OS integration. Mounted inside WorkspaceSlugProvider so it can resolve the
 * current workspace's id for the badge hook.
 *
 * Two responsibilities:
 *   1. Mirror the unread inbox count onto the dock/taskbar badge.
 *   2. When the user clicks an OS notification, open the notified
 *      workspace's inbox focused on that item. The route uses the `slug`
 *      that the notification was *emitted* with — not the currently active
 *      workspace — so a notification from workspace A always opens A's
 *      inbox even if the user has since switched to workspace B. Marking
 *      the row read is handled by InboxPage's selected-item effect, which
 *      covers both click-to-select and URL-param-select paths.
 *
 * The click routes through `useNavigation().push` — NOT the
 * `multica:navigate` event, whose handler `openTab`s into the ACTIVE
 * workspace's tab group. The navigation adapter detects a cross-workspace
 * path and translates it into `switchWorkspace(slug, path)`, so clicking a
 * workspace-A notification while B is active performs a real workspace
 * switch instead of mounting A's inbox inside B's tab group (#3766).
 */
function DesktopInboxBridge() {
  const workspace = useCurrentWorkspace();
  useDesktopUnreadBadge(workspace?.id ?? null);
  const { push } = useNavigation();
  // The adapter identity changes with the active tab's location; the ref
  // keeps the main-process subscription stable across navigations.
  const pushRef = useRef(push);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);

  useEffect(() => {
    return window.desktopAPI.onInboxOpen(({ slug, issueKey }) => {
      if (!slug) return;
      const inboxPath = `${paths.workspace(slug).inbox()}?issue=${encodeURIComponent(issueKey)}`;
      pushRef.current(inboxPath);
    });
  }, []);

  return null;
}

export function DesktopShell() {
  useInternalLinkHandler();
  useNativeNavigationGestures();
  useNavigationInputBindings();

  // Reactive read of current workspace slug from the platform singleton.
  // On first mount, it is null until WorkspaceRouteLayout (inside the tab
  // router) sets it. Once set, the rail and other shell-level components
  // can resolve workspace-scoped paths via useWorkspacePaths().
  const currentSlug = useSyncExternalStore(
    subscribeToCurrentSlug,
    getCurrentSlug,
    () => null,
  );
  // Chrome gates on "the slug still resolves to a workspace", NOT on "the
  // singleton is non-null" (MUL-6231 / #7021). The singleton is mutable
  // process state that no single owner keeps in lockstep with the workspace
  // list, so after the active workspace is deleted it can still hold the dead
  // slug for a beat. Everything below mounts workspace-scoped components —
  // SearchCommand calls useWorkspaceId(), which THROWS when the workspace is
  // gone from the list. Nothing above this in the desktop tree is an error
  // boundary, so that throw used to unmount the whole renderer and leave a
  // blank, unresponsive window.
  //
  // Deriving from the list cache makes this the same gate web uses
  // (DashboardGuard's `!workspace` check in packages/views/layout), so both
  // shells drop workspace-scoped chrome on exactly the same signal instead of
  // diverging. TabContent stays outside the gate: it must always render so
  // the tab router can mount WorkspaceRouteLayout, which is what populates
  // the singleton in the first place.
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const slug =
    currentSlug && workspaces.some((w) => w.slug === currentSlug)
      ? currentSlug
      : null;
  const renderedPathname = useRenderedRouteStore((s) => s.pathname);

  return (
    <DesktopNavigationProvider>
      {/* WorkspaceSlugProvider accepts null — components that need slug
          use useWorkspaceSlug() (nullable) or useRequiredWorkspaceSlug()
          (throws). TabContent MUST always render so the tab router can
          mount WorkspaceRouteLayout, which calls setCurrentWorkspace()
          to populate the slug. The rail gates on the resolved slug
          (see above) to avoid the useRequiredWorkspaceSlug and
          useWorkspaceId throws. Zero-workspace users see the
          window-level overlay (new-workspace flow) triggered by
          IndexRedirect, not a route. */}
      <WorkspaceSlugProvider slug={slug}>
        <DesktopInboxBridge />
        <div className="flex h-screen bg-app-shell">
          {/* bg-app-shell is the wrapper's non-inset fill, so it also owns the
              non-inset half of --sidebar-wrapper-fill. sidebar.tsx supplies the
              inset half of both. Anything that has to paint an opaque layer
              over this wrapper (the tab flares) reads the variable rather than
              re-deriving which of the two is in play. */}
          {/* hasExternalTrigger: WindowToolbar below parks a SidebarTrigger
              beside the traffic lights, where it is always reachable. Page
              headers inside the canvas must not add their own fallback one on
              top of it — desktop windows often sit in the band where that
              fallback renders, so every page showed a second identical icon
              50px under this one (MUL-6218). */}
          <SidebarProvider
            hasExternalTrigger
            className="flex-1 bg-app-shell [--sidebar-wrapper-fill:var(--app-shell)]"
          >
            <AppShellProvider renderedPathname={renderedPathname}>
              <ShellRouteDataPrefetch>
                {slug && <GlobalShortcuts />}
                {slug && <WindowToolbar />}
                {slug && (
                  <AppRail
                    topSlot={<RailTopSpacer />}
                    globalActions={false}
                    className="hidden pt-0 md:flex"
                  />
                )}
                {slug && <CompactNavSheet globalActions={false} />}
                {/* Right side: header + content card */}
                <div className="flex flex-1 min-w-0 flex-col">
                  <MainTopBar railMounted={Boolean(slug)} />
                  <MainCanvas railMounted={Boolean(slug)} showWorkspaceLoading={!slug}>
                    {/* Same indicator, same anchor as web: DashboardLayout puts
                        it at the top of the content column, and this is
                        desktop's equivalent relative/overflow-hidden box.
                        Desktop used to have no navigation feedback at all — a
                        click just froze until the destination committed
                        (MUL-6404). */}
                    <NavigationProgress />
                    <TabContent />
                    {slug && <FloatingChat />}
                  </MainCanvas>
                </div>
              </ShellRouteDataPrefetch>
            </AppShellProvider>
          </SidebarProvider>
        </div>
        {slug && <ModalRegistry />}
        {slug && <SearchCommand />}
        <WindowOverlay />
      </WorkspaceSlugProvider>
    </DesktopNavigationProvider>
  );
}

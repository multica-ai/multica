import { useEffect, useRef, useSyncExternalStore } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { motion } from "motion/react";
import { cn } from "@multica/ui/lib/utils";
import { useTabHistory } from "@/hooks/use-tab-history";
import { useActiveTitleSync } from "@/hooks/use-tab-sync";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";
import {
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "@multica/views/modals/registry";
import { AppSidebar, GlobalShortcuts } from "@multica/views/layout";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { FloatingChat } from "@multica/views/chat";
import { WorkspaceSlugProvider, paths, useCurrentWorkspace } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { getCurrentSlug, subscribeToCurrentSlug } from "@multica/core/platform";
import { useDesktopUnreadBadge } from "@multica/views/platform";
import { DesktopNavigationProvider } from "@/platform/navigation";
import { TabBar } from "./tab-bar";
import { TabContent } from "./tab-content";
import { WindowOverlay } from "./window-overlay";

const TOP_BAR_HEIGHT_CLASS = "h-12";
const WINDOW_TOOLBAR_CLEARANCE = 184;
const toolbarMotion = {
  type: "spring",
  stiffness: 420,
  damping: 38,
  mass: 0.8,
} as const;

function WindowToolbar() {
  const { canGoBack, canGoForward, goBack, goForward } = useTabHistory();
  const navButtonClassName =
    "flex size-7 items-center justify-center rounded-md text-muted-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground disabled:pointer-events-none disabled:opacity-30";

  return (
    <div
      className={cn(
        "fixed left-0 top-0 z-30 flex w-[184px] shrink-0 items-center px-3",
        TOP_BAR_HEIGHT_CLASS,
      )}
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div
        className="flex items-center gap-1 pl-[70px]"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        <SidebarTrigger
          className="size-7 text-muted-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
        />
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={goBack}
            disabled={!canGoBack}
            aria-label="Go back"
            title="Go back"
            className={navButtonClassName}
            style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          >
            <ChevronLeft className="size-4" />
          </button>
          <button
            type="button"
            onClick={goForward}
            disabled={!canGoForward}
            aria-label="Go forward"
            title="Go forward"
            className={navButtonClassName}
            style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          >
            <ChevronRight className="size-4" />
          </button>
        </div>
      </div>
    </div>
  );
}

function SidebarTopSpacer() {
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

// The main area's top bar doubles as a window drag region. When the sidebar
// is not occupying main-flow width, leave room for the fixed window toolbar
// so tabs do not land beneath the traffic lights / navigation controls.
function MainTopBar() {
  const { state, isMobile } = useSidebar();
  const sidebarHidden = state === "collapsed" || isMobile;

  return (
    <motion.header
      animate={{ paddingLeft: sidebarHidden ? WINDOW_TOOLBAR_CLEARANCE : 0 }}
      className={cn("relative shrink-0 flex items-center gap-2", TOP_BAR_HEIGHT_CLASS)}
      initial={false}
      transition={toolbarMotion}
    >
      <motion.div
        aria-hidden
        animate={{ left: sidebarHidden ? WINDOW_TOOLBAR_CLEARANCE : 0 }}
        className="absolute inset-y-0 right-0"
        initial={false}
        transition={toolbarMotion}
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      />
      <div className="relative z-10 flex h-full min-w-0 max-w-full items-center">
        <TabBar />
      </div>
    </motion.header>
  );
}

// The canvas hugs the expanded sidebar with a hairline gap. When the sidebar
// leaves the main flow, the left margin must grow to mirror the fixed mr-2 so
// the floating canvas sits symmetrically inside the window frame.
function MainCanvas({ children }: { children: React.ReactNode }) {
  const { state, isMobile } = useSidebar();
  const sidebarHidden = state === "collapsed" || isMobile;

  return (
    <motion.div
      animate={{ marginLeft: sidebarHidden ? 8 : 2 }}
      className="relative flex flex-1 min-h-0 flex-col overflow-hidden mr-2 mb-2 rounded-xl bg-page-canvas ring-1 ring-surface-border shadow-[var(--surface-shadow)]"
      initial={false}
      transition={toolbarMotion}
    >
      {children}
    </motion.div>
  );
}

function useInternalLinkHandler() {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent).detail?.path;
      if (!path) return;
      const icon = resolveRouteIcon(path);
      const store = useTabStore.getState();
      const tabId = store.openTab(path, path, icon);
      store.setActiveTab(tabId);
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
  useActiveTitleSync();
  useNativeNavigationGestures();

  // Reactive read of current workspace slug from the platform singleton.
  // On first mount, slug is null until WorkspaceRouteLayout (inside the tab
  // router) sets it. Once set, the sidebar and other shell-level components
  // can resolve workspace-scoped paths via useWorkspacePaths().
  const slug = useSyncExternalStore(subscribeToCurrentSlug, getCurrentSlug, () => null);

  return (
    <DesktopNavigationProvider>
      {/* WorkspaceSlugProvider accepts null — components that need slug
          use useWorkspaceSlug() (nullable) or useRequiredWorkspaceSlug()
          (throws). TabContent MUST always render so the tab router can
          mount WorkspaceRouteLayout, which calls setCurrentWorkspace()
          to populate the slug. The sidebar gates on slug being present
          to avoid the useRequiredWorkspaceSlug throw. Zero-workspace
          users see the window-level overlay (new-workspace flow)
          triggered by IndexRedirect, not a route. */}
      <WorkspaceSlugProvider slug={slug}>
        <DesktopInboxBridge />
        <div className="flex h-screen bg-app-shell">
          <SidebarProvider className="flex-1 bg-app-shell">
            {slug && <GlobalShortcuts />}
            {slug && <WindowToolbar />}
            {slug && <AppSidebar topSlot={<SidebarTopSpacer />} searchSlot={<SearchTrigger />} />}
            {/* Right side: header + content container */}
            <motion.div layout transition={toolbarMotion} className="flex flex-1 min-w-0 flex-col">
              <MainTopBar />
              <MainCanvas>
                <TabContent />
                {slug && <FloatingChat />}
              </MainCanvas>
            </motion.div>
          </SidebarProvider>
        </div>
        {slug && <ModalRegistry />}
        {slug && <SearchCommand />}
        <WindowOverlay />
      </WorkspaceSlugProvider>
    </DesktopNavigationProvider>
  );
}

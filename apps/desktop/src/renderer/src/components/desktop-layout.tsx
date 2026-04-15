import { useEffect } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useTabHistory } from "@/hooks/use-tab-history";
import { useActiveTitleSync } from "@/hooks/use-tab-sync";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";
import {
  SidebarProvider,
  useSidebar,
} from "@multica/ui/components/ui/sidebar";
import { ModalRegistry } from "@multica/views/modals/registry";
import { AppSidebar, DashboardGuard } from "@multica/views/layout";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { ChatFab, ChatWindow } from "@multica/views/chat";
import { StepWorkspace } from "@multica/views/onboarding";
import { useWorkspaceStore } from "@multica/core/workspace";
import { DesktopNavigationProvider } from "@/platform/navigation";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { OnboardingGate } from "./onboarding-gate";
import { TabBar } from "./tab-bar";
import { TabContent } from "./tab-content";

function SidebarTopBar() {
  const { canGoBack, canGoForward, goBack, goForward } = useTabHistory();

  return (
    <div
      className="h-12 shrink-0 flex items-center justify-end px-2"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <div
        className="flex items-center gap-0.5"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        <button
          onClick={goBack}
          disabled={!canGoBack}
          aria-label="Go back"
          className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
        >
          <ChevronLeft className="size-4" />
        </button>
        <button
          onClick={goForward}
          disabled={!canGoForward}
          aria-label="Go forward"
          className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
        >
          <ChevronRight className="size-4" />
        </button>
      </div>
    </div>
  );
}

// The main area's top bar doubles as a window drag region. When the sidebar
// is collapsed, we pad the left side so tabs don't land under the macOS
// traffic lights (which live at roughly x=16..68 and always hit-test above HTML).
function MainTopBar() {
  const { state } = useSidebar();
  const sidebarCollapsed = state === "collapsed";

  return (
    <header
      className={cn("h-12 shrink-0", sidebarCollapsed && "pl-20")}
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      <TabBar />
    </header>
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

export function DesktopShell() {
  useInternalLinkHandler();
  useActiveTitleSync();

  const workspace = useWorkspaceStore((s) => s.workspace);

  return (
    <DesktopNavigationProvider>
      <OnboardingGate
        hasWorkspace={!!workspace}
        onboarding={(onComplete) => (
          <div className="flex min-h-screen items-center justify-center overflow-auto bg-background px-6 py-12">
            <StepWorkspace onNext={onComplete} />
          </div>
        )}
      >
        <DashboardGuard
          loginPath="/login"
          loadingFallback={
            <div className="flex h-screen items-center justify-center">
              <MulticaIcon className="size-6 animate-pulse" />
            </div>
          }
        >
          <div className="flex h-screen">
            <SidebarProvider className="flex-1">
              <AppSidebar topSlot={<SidebarTopBar />} searchSlot={<SearchTrigger />} />
              {/* Right side: header + content container */}
              <div className="flex flex-1 min-w-0 flex-col">
                <MainTopBar />
                {/* Content area with inset styling — relative so ChatWindow/ChatFab are constrained here */}
                <div className="relative flex flex-1 min-h-0 flex-col overflow-hidden mr-2 mb-2 ml-0.5 rounded-xl shadow-sm bg-background">
                  <TabContent />
                  <ChatWindow />
                  <ChatFab />
                </div>
              </div>
            </SidebarProvider>
          </div>
          <ModalRegistry />
          <SearchCommand />
        </DashboardGuard>
      </OnboardingGate>
    </DesktopNavigationProvider>
  );
}

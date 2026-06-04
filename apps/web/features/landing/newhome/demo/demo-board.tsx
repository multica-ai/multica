"use client";

import { useMemo, useRef, useState } from "react";
import { ArrowLeft, LayoutList, Bot, Sparkles } from "lucide-react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { PortalContainerProvider } from "@multica/ui/lib/portal-container";
import { setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useIssueViewStore } from "@multica/core/issues/stores/view-store";
import { RESOURCES } from "@multica/views/locales";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { IssuesPage, IssueDetail } from "@multica/views/issues/components";
import { ModalRegistry } from "@multica/views/modals/registry";
import { createMockApi } from "./mock-api";
import { WORKSPACE } from "./mock-data";
import { DemoErrorBoundary } from "./demo-error-boundary";
import { AgentsPanel, SkillsPanel } from "./demo-panels";

// Install the mock client globally (the @multica/core/api singleton). This
// module is only ever imported client-side via a dynamic ssr:false import,
// and only on the landing page, so overriding the singleton here is safe.
setApiInstance(createMockApi());

const ISSUE_PATH = /\/issues\/([^/?#]+)/;

const TABS = [
  { id: "issues", label: "Issues", Icon: LayoutList },
  { id: "agents", label: "Agents", Icon: Bot },
  { id: "skills", label: "Skills", Icon: Sparkles },
] as const;
type TabId = (typeof TABS)[number]["id"];

export function DemoBoard() {
  const [tab, setTab] = useState<TabId>("issues");
  const [detailId, setDetailId] = useState<string | null>(null);

  // Keep the latest setter in a ref so the navigation adapter is stable.
  const setDetailRef = useRef(setDetailId);
  setDetailRef.current = setDetailId;

  // Portal mount for popups (menus, dialogs, hover cards, tooltips). It lives
  // inside the scaled demo box, so every portaled popup inherits the same zoom
  // instead of rendering at 1:1 over the page.
  const portalRef = useRef<HTMLDivElement>(null);

  const queryClient = useMemo(() => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false, refetchOnWindowFocus: false, staleTime: 30_000 },
        mutations: { retry: false },
      },
    });
    qc.setQueryData(workspaceListOptions().queryKey, [WORKSPACE]);
    return qc;
  }, []);

  // No status filter — show every column (backlog … blocked). Reset on mount in
  // case a previous session persisted a filter.
  useState(() => {
    useIssueViewStore.setState({ statusFilters: [] });
    return true;
  });

  const adapter = useMemo<NavigationAdapter>(() => {
    const openFromPath = (path: string) => {
      const m = path.match(ISSUE_PATH);
      if (m?.[1]) setDetailRef.current(decodeURIComponent(m[1]));
    };
    return {
      push: openFromPath,
      replace: openFromPath,
      back: () => setDetailRef.current(null),
      pathname: "/demo/issues",
      searchParams: new URLSearchParams(),
      getShareableUrl: (p) => p,
    };
  }, []);

  const resources = useMemo(() => ({ en: RESOURCES.en }), []);

  return (
    <DemoErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <I18nProvider locale="en" resources={resources}>
          <NavigationProvider value={adapter}>
            <WorkspaceSlugProvider slug="demo">
            <PortalContainerProvider container={portalRef}>
              {/* `landing-demo` darkens --brand so the selected "working" chip
                  stays readable (white-on-brand). */}
              <div className="landing-demo flex h-full w-full flex-col bg-background text-foreground">
                <BrowserBar
                  tab={tab}
                  onTab={(t) => {
                    setTab(t);
                    setDetailId(null);
                  }}
                />
                <div className="min-h-0 flex-1">
                  {tab === "issues" ? (
                    detailId ? (
                      <div className="flex h-full flex-col">
                        <button
                          type="button"
                          onClick={() => setDetailId(null)}
                          className="flex shrink-0 items-center gap-1.5 px-4 py-2.5 text-[13px] font-medium text-muted-foreground hover:text-foreground"
                        >
                          <ArrowLeft className="size-4" aria-hidden />
                          Back to board
                        </button>
                        <div className="min-h-0 flex-1 overflow-auto [scrollbar-width:thin]">
                          <IssueDetail
                            issueId={detailId}
                            onDone={() => setDetailId(null)}
                            onDelete={() => setDetailId(null)}
                          />
                        </div>
                      </div>
                    ) : (
                      // Hide IssuesPage's own PageHeader — the browser tabs
                      // above already serve as the app header.
                      <div className="flex h-full w-full flex-col [&>div>div:first-child]:hidden">
                        <IssuesPage />
                      </div>
                    )
                  ) : tab === "agents" ? (
                    <AgentsPanel />
                  ) : (
                    <SkillsPanel />
                  )}
                </div>
                {/* Popups (menus, dialogs, hover cards, tooltips) portal here
                    via PortalContainerProvider, so they share the demo's zoom
                    instead of rendering at 1:1 over the page. */}
                <div ref={portalRef} />
              </div>
              {/* Real create-issue dialog host — opened by the board's "+"
                  buttons via the global modal store. Portals into the scaled
                  box (see PortalContainerProvider) so it matches the demo zoom. */}
              <DemoErrorBoundary fallback={null}>
                <ModalRegistry />
              </DemoErrorBoundary>
            </PortalContainerProvider>
            </WorkspaceSlugProvider>
          </NavigationProvider>
        </I18nProvider>
      </QueryClientProvider>
    </DemoErrorBoundary>
  );
}

function BrowserBar({
  tab,
  onTab,
}: {
  tab: TabId;
  onTab: (t: TabId) => void;
}) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-3 border-b border-[#0a0d12]/8 bg-[#f7f8fa] px-3.5">
      <div className="flex shrink-0 items-center gap-1.5">
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
      </div>
      <div className="flex items-center gap-0.5">
        {TABS.map(({ id, label, Icon }) => (
          <button
            key={id}
            type="button"
            onClick={() => onTab(id)}
            className={cn(
              "inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2.5 text-[13px] font-medium transition-colors",
              tab === id
                ? "bg-white text-[#0a0d12] shadow-[0_1px_2px_rgba(10,13,18,0.08)] ring-1 ring-[#0a0d12]/8"
                : "text-[#0a0d12]/55 hover:bg-[#0a0d12]/[0.04] hover:text-[#0a0d12]/80",
            )}
          >
            <Icon className="size-3.5" aria-hidden />
            {label}
          </button>
        ))}
      </div>
    </div>
  );
}

"use client";

import { useEffect, useMemo, useRef, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  BookOpen,
  Bot,
  LayoutList,
  Server,
  Sparkles,
  UsersRound,
} from "lucide-react";
import { setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { useIssueViewStore } from "@multica/core/issues/stores/view-store";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { agentTaskSnapshotKeys } from "@multica/core/agents";
import { runtimeKeys } from "@multica/core/runtimes";
import {
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { PortalContainerProvider } from "@multica/ui/lib/portal-container";
import { cn } from "@multica/ui/lib/utils";
import { RESOURCES } from "@multica/views/locales";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";
import { ModalRegistry } from "@multica/views/modals/registry";
import { createMockApi } from "./mock-api";
import {
  AGENTS,
  MEMBERS,
  RUNTIMES,
  RUNNING_TASKS,
  SKILLS,
  SQUADS,
  WORKSPACE,
} from "./mock-data";
import { DemoErrorBoundary } from "./demo-error-boundary";

setApiInstance(createMockApi());

export type DemoProductTab = "issues" | "runtimes" | "agents" | "squads" | "skills";

const PRODUCT_TABS = [
  { id: "issues", label: "Issues", Icon: LayoutList },
  { id: "runtimes", label: "Runtimes", Icon: Server },
  { id: "agents", label: "Agents", Icon: Bot },
  { id: "squads", label: "Squads", Icon: UsersRound },
  { id: "skills", label: "Skills", Icon: BookOpen },
] as const;

export function DemoProductFrame({
  activeTab,
  pathname,
  children,
  className,
}: {
  activeTab: DemoProductTab;
  pathname: string;
  children: ReactNode;
  className?: string;
}) {
  const portalRef = useRef<HTMLDivElement>(null);
  const queryClient = useMemo(() => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false, refetchOnWindowFocus: false, staleTime: 30_000 },
        mutations: { retry: false },
      },
    });
    seedDemoQueryData(qc);
    return qc;
  }, []);
  const resources = useMemo(() => ({ en: RESOURCES.en }), []);

  useEffect(() => {
    useIssueViewStore.setState({ statusFilters: [] });
  }, []);

  const adapter = useMemo<NavigationAdapter>(
    () => ({
      push: () => {},
      replace: () => {},
      back: () => {},
      pathname,
      searchParams: new URLSearchParams(),
      getShareableUrl: (p) => p,
    }),
    [pathname],
  );

  return (
    <DemoErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <I18nProvider locale="en" resources={resources}>
          <NavigationProvider value={adapter}>
            <WorkspaceSlugProvider slug="demo">
              <PortalContainerProvider container={portalRef}>
                <div
                  className={cn(
                    "landing-demo flex h-full w-full flex-col bg-background text-foreground",
                    className,
                  )}
                >
                  <DemoBrowserBar activeTab={activeTab} />
                  <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
                  <div ref={portalRef} />
                </div>
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

export function DemoBrowserBar({ activeTab }: { activeTab: DemoProductTab }) {
  return (
    <div className="flex h-11 shrink-0 items-center gap-3 border-b border-[#0a0d12]/8 bg-[#f7f8fa] px-3.5">
      <div className="flex shrink-0 items-center gap-1.5">
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
        <span className="size-2.5 rounded-full bg-[#0a0d12]/12" />
      </div>
      <div className="flex min-w-0 items-center gap-0.5 overflow-hidden">
        {PRODUCT_TABS.map(({ id, label, Icon }) => (
          <span
            key={id}
            className={cn(
              "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-[8px] px-2.5 text-[13px] font-medium transition-colors",
              activeTab === id
                ? "bg-white text-[#0a0d12] shadow-[0_1px_2px_rgba(10,13,18,0.08)] ring-1 ring-[#0a0d12]/8"
                : "text-[#0a0d12]/55",
            )}
          >
            <Icon className="size-3.5" aria-hidden />
            {label}
          </span>
        ))}
      </div>
      <Sparkles
        className="ml-auto hidden size-3.5 shrink-0 text-[#0a0d12]/30 sm:block"
        aria-hidden
      />
    </div>
  );
}

function seedDemoQueryData(qc: QueryClient) {
  const wsId = WORKSPACE.id;
  qc.setQueryData(workspaceListOptions().queryKey, [WORKSPACE]);
  qc.setQueryData(workspaceKeys.members(wsId), MEMBERS);
  qc.setQueryData(workspaceKeys.agents(wsId), AGENTS);
  qc.setQueryData(workspaceKeys.squads(wsId), SQUADS);
  qc.setQueryData(workspaceKeys.skills(wsId), SKILLS);
  qc.setQueryData(runtimeKeys.list(wsId), RUNTIMES);
  qc.setQueryData(agentTaskSnapshotKeys.list(wsId), RUNNING_TASKS);
}

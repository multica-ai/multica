"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import { providerSupportsMcpConfig } from "@multica/core/agents";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { ActivityTab } from "./tabs/activity-tab";
import { InstructionsTab } from "./tabs/instructions-tab";
import { SkillsTab } from "./tabs/skills-tab";
import { EnvTab } from "./tabs/env-tab";
import { CustomArgsTab } from "./tabs/custom-args-tab";
import { McpConfigTab } from "./tabs/mcp-config-tab";
import { AgentMcpTab } from "./tabs/agent-mcp-tab";
import { IntegrationsTab } from "./tabs/integrations-tab";
import { RuntimeConfigTab } from "./tabs/runtime-config-tab";
import { AgentDetailInspector } from "./agent-detail-inspector";
import { AgentAccessSettings } from "./agent-access-settings";
import { AgentOverviewSummary } from "./agent-overview-summary";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

type DetailSection = "overview" | "work" | "capabilities" | "settings";

export type DetailTab =
  | "overview"
  | "work"
  | "instructions"
  | "skills"
  | "mcp_config"
  | "composio_mcp"
  | "integrations"
  | "general"
  | "access"
  | "env"
  | "custom_args"
  | "runtime_config";

type SecondaryTab = {
  id: DetailTab;
  labelKey:
    | "instructions"
    | "skills"
    | "mcp_config"
    | "composio_mcp"
    | "integrations"
    | "general"
    | "access"
    | "environment"
    | "custom_args"
    | "runtime_config";
};

const CAPABILITY_TABS: SecondaryTab[] = [
  { id: "instructions", labelKey: "instructions" },
  { id: "skills", labelKey: "skills" },
  { id: "mcp_config", labelKey: "mcp_config" },
  { id: "composio_mcp", labelKey: "composio_mcp" },
  { id: "integrations", labelKey: "integrations" },
];

const SETTINGS_TABS: SecondaryTab[] = [
  { id: "general", labelKey: "general" },
  { id: "access", labelKey: "access" },
  { id: "env", labelKey: "environment" },
  { id: "custom_args", labelKey: "custom_args" },
  { id: "runtime_config", labelKey: "runtime_config" },
];

const TOP_TABS: { id: DetailSection; labelKey: DetailSection }[] = [
  { id: "overview", labelKey: "overview" },
  { id: "work", labelKey: "work" },
  { id: "capabilities", labelKey: "capabilities" },
  { id: "settings", labelKey: "settings" },
];

const CAPABILITY_IDS = new Set<DetailTab>(
  CAPABILITY_TABS.map((tab) => tab.id),
);
const SETTINGS_IDS = new Set<DetailTab>(SETTINGS_TABS.map((tab) => tab.id));
const DETAIL_VIEWS = new Set<DetailTab>([
  "overview",
  "work",
  ...CAPABILITY_TABS.map((tab) => tab.id),
  ...SETTINGS_TABS.map((tab) => tab.id),
]);

function isDetailTab(value: string | null): value is DetailTab {
  return value !== null && DETAIL_VIEWS.has(value as DetailTab);
}

function sectionForView(view: DetailTab): DetailSection {
  if (view === "overview") return "overview";
  if (view === "work") return "work";
  if (CAPABILITY_IDS.has(view)) return "capabilities";
  return "settings";
}

interface AgentOverviewPaneProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  currentUserId?: string | null;
  canEdit: boolean;
  navIntent?: DetailTab | null;
  onNavIntentHandled?: () => void;
}

/**
 * Agent workbench organised around user intent instead of backend fields.
 * Overview answers "what is happening now?", Work owns the issue surface,
 * Capabilities describes what the agent can do, and Settings describes how
 * it runs. The lower-level editors stay intact so the reorganisation does not
 * alter persistence or permission semantics.
 */
export function AgentOverviewPane({
  agent,
  runtime,
  owner,
  runtimes,
  members,
  onUpdate,
  currentUserId,
  canEdit,
  navIntent,
  onNavIntentHandled,
}: AgentOverviewPaneProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const urlView = navigation.searchParams.get("view");
  const composioMCPAppsEnabled = useFeatureEnabled(
    COMPOSIO_MCP_APPS_FLAG,
    false,
  );
  const [activeView, setActiveView] = useState<DetailTab>(() =>
    isDetailTab(urlView) ? urlView : "overview",
  );
  const [activeDirty, setActiveDirty] = useState(false);
  const [pendingView, setPendingView] = useState<DetailTab | null>(null);
  const lastUrlViewRef = useRef(urlView);

  const { data: larkListing } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: slackListing } = useQuery({
    ...slackInstallationsOptions(wsId),
    enabled: !!wsId,
  });

  const integrationsConfigured =
    larkListing?.configured === true || slackListing?.configured === true;

  const visibleCapabilityTabs = useMemo(() => {
    const showMcp = runtime
      ? providerSupportsMcpConfig(runtime.provider)
      : true;
    const showComposioMcp =
      composioMCPAppsEnabled &&
      !!currentUserId &&
      !!agent.owner_id &&
      agent.owner_id === currentUserId;

    return CAPABILITY_TABS.filter((tab) => {
      if (tab.id === "mcp_config") return showMcp;
      if (tab.id === "composio_mcp") return showComposioMcp;
      if (tab.id === "integrations") return integrationsConfigured;
      return true;
    });
  }, [
    agent.owner_id,
    composioMCPAppsEnabled,
    currentUserId,
    integrationsConfigured,
    runtime,
  ]);

  const visibleSettingsTabs = useMemo(
    () =>
      SETTINGS_TABS.filter(
        (tab) => tab.id !== "runtime_config" || runtime?.provider === "openclaw",
      ),
    [runtime?.provider],
  );

  const visibleViews = useMemo(
    () =>
      new Set<DetailTab>([
        "overview",
        "work",
        ...visibleCapabilityTabs.map((tab) => tab.id),
        ...visibleSettingsTabs.map((tab) => tab.id),
      ]),
    [visibleCapabilityTabs, visibleSettingsTabs],
  );

  const effectiveView = visibleViews.has(activeView) ? activeView : "overview";
  const activeSection = sectionForView(effectiveView);

  const commitView = useCallback(
    (next: DetailTab) => {
      setActiveView(next);
      const params = new URLSearchParams(navigation.searchParams);
      if (next === "overview") params.delete("view");
      else params.set("view", next);
      const query = params.toString();
      navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
    },
    [navigation],
  );

  const requestView = useCallback(
    (next: DetailTab) => {
      if (next === effectiveView) return;
      if (activeDirty) {
        setPendingView(next);
        return;
      }
      commitView(next);
    },
    [activeDirty, commitView, effectiveView],
  );

  const requestSection = (section: DetailSection) => {
    if (section === "overview" || section === "work") {
      requestView(section);
      return;
    }
    if (section === "capabilities") {
      const current = CAPABILITY_IDS.has(effectiveView)
        ? effectiveView
        : visibleCapabilityTabs[0]?.id;
      if (current) requestView(current);
      return;
    }
    const current = SETTINGS_IDS.has(effectiveView)
      ? effectiveView
      : visibleSettingsTabs[0]?.id;
    if (current) requestView(current);
  };

  const commitViewChange = () => {
    if (!pendingView) return;
    commitView(pendingView);
    setActiveDirty(false);
    setPendingView(null);
  };

  useEffect(() => {
    if (urlView === lastUrlViewRef.current) return;
    lastUrlViewRef.current = urlView;
    if (urlView === null) {
      setActiveView("overview");
      return;
    }
    if (isDetailTab(urlView) && visibleViews.has(urlView)) {
      setActiveView(urlView);
    }
  }, [urlView, visibleViews]);

  useEffect(() => {
    if (navIntent == null) return;
    if (visibleViews.has(navIntent)) requestView(navIntent);
    onNavIntentHandled?.();
  }, [navIntent, onNavIntentHandled, requestView, visibleViews]);

  const secondaryTabs =
    activeSection === "capabilities"
      ? visibleCapabilityTabs
      : activeSection === "settings"
        ? visibleSettingsTabs
        : [];
  const activeSecondaryTab = secondaryTabs.find(
    (tab) => tab.id === effectiveView,
  );
  const isSecondaryLayout = secondaryTabs.length > 0 && activeSecondaryTab != null;

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <div
        className="shrink-0 overflow-x-auto border-b px-4 sm:px-6"
        role="tablist"
        aria-label={t(($) => $.tabs.page_navigation_aria)}
      >
        <div className="mx-auto flex max-w-[1440px] items-center gap-6">
          {TOP_TABS.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={activeSection === tab.id}
              onClick={() => requestSection(tab.id)}
              className={cn(
                "relative shrink-0 py-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                activeSection === tab.id
                  ? "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t(($) => $.tabs[tab.labelKey])}
            </button>
          ))}
        </div>
      </div>

      {/* Overview/Work scroll as one page. Sidebar views split scrolling on
          md+ (nav rail pinned, content pane scrolls) like settings-page.tsx;
          below md the rail is a horizontal strip and the page scrolls whole. */}
      <div
        className={cn(
          "min-h-0 flex-1 overflow-y-auto",
          isSecondaryLayout && "md:overflow-hidden",
        )}
      >
        {effectiveView === "overview" && (
          <div className="mx-auto max-w-[1440px] p-4 sm:p-6">
            <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
              <ActivityTab agent={agent} showPerformance={false} />
              <AgentOverviewSummary
                agent={agent}
                runtime={runtime}
                owner={owner}
              />
            </div>
          </div>
        )}

        {effectiveView === "work" && (
          <div className="flex min-h-[620px] flex-col">
            <ActorIssuesPanel actorType="agent" actorId={agent.id} />
          </div>
        )}

        {secondaryTabs.length > 0 && activeSecondaryTab && (
          <div className="flex min-h-full flex-col md:h-full md:flex-row">
            {/* Content-surface color, no shell tint — same rule as the settings
                nav: in-card panels must not break the desktop tab merge (MUL-4439). */}
            <aside className="shrink-0 overflow-x-auto border-b border-surface-border p-2 md:w-52 md:overflow-y-auto md:border-b-0 md:border-r md:p-4">
              <div
                className="flex w-max min-w-full items-center gap-1 md:w-full md:flex-col md:items-stretch"
                role="tablist"
                aria-orientation="vertical"
                aria-label={t(($) => $.tabs.section_navigation_aria)}
              >
                {secondaryTabs.map((tab) => {
                  const active = effectiveView === tab.id;
                  return (
                    <button
                      key={tab.id}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      onClick={() => requestView(tab.id)}
                      className={cn(
                        "flex h-8 shrink-0 items-center rounded-md px-2.5 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:w-full",
                        active
                          ? "bg-surface-selected font-medium text-surface-selected-foreground hover:bg-surface-selected"
                          : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
                      )}
                    >
                      {t(($) => $.tabs[tab.labelKey])}
                    </button>
                  );
                })}
              </div>
            </aside>

            <section className="min-w-0 flex-1 md:overflow-y-auto">
              <div className="mx-auto w-full max-w-3xl p-4 sm:p-6 md:p-8">
                <header>
                  <h2 className="text-base font-medium text-balance">
                    {t(($) => $.tabs[activeSecondaryTab.labelKey])}
                  </h2>
                </header>

                <div className="mt-6">
                  {effectiveView === "instructions" && (
                    <InstructionsTab
                      agent={agent}
                      onSave={(instructions) =>
                        onUpdate(agent.id, { instructions })
                      }
                      onDirtyChange={setActiveDirty}
                    />
                  )}
                  {effectiveView === "skills" && (
                    <SkillsTab
                      agent={agent}
                      runtime={runtime}
                      canEdit={canEdit}
                    />
                  )}
                  {effectiveView === "mcp_config" && (
                    <McpConfigTab
                      agent={agent}
                      runtime={runtime}
                      onSave={(updates) => onUpdate(agent.id, updates)}
                      onDirtyChange={setActiveDirty}
                    />
                  )}
                  {effectiveView === "composio_mcp" && (
                    <AgentMcpTab agent={agent} />
                  )}
                  {effectiveView === "integrations" && (
                    <IntegrationsTab agent={agent} />
                  )}
                  {effectiveView === "general" && (
                    <AgentDetailInspector
                      agent={agent}
                      runtime={runtime}
                      runtimes={runtimes}
                      members={members}
                      currentUserId={currentUserId ?? null}
                      canEdit={canEdit}
                      onUpdate={onUpdate}
                    />
                  )}
                  {effectiveView === "access" && (
                    <AgentAccessSettings
                      agent={agent}
                      members={members}
                      currentUserId={currentUserId ?? null}
                      onDirtyChange={setActiveDirty}
                      onUpdate={onUpdate}
                    />
                  )}
                  {effectiveView === "env" && (
                    <EnvTab agent={agent} onDirtyChange={setActiveDirty} />
                  )}
                  {effectiveView === "custom_args" && (
                    <CustomArgsTab
                      agent={agent}
                      runtimeDevice={runtime ?? undefined}
                      onSave={(updates) => onUpdate(agent.id, updates)}
                      onDirtyChange={setActiveDirty}
                    />
                  )}
                  {effectiveView === "runtime_config" && (
                    <RuntimeConfigTab
                      agent={agent}
                      onSave={(updates) => onUpdate(agent.id, updates)}
                      onDirtyChange={setActiveDirty}
                    />
                  )}
                </div>
              </div>
            </section>
          </div>
        )}
      </div>

      {pendingView !== null && (
        <AlertDialog
          open
          onOpenChange={(open) => {
            if (!open) setPendingView(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.tabs.discard_dialog_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tabs.discard_dialog_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>
                {t(($) => $.tabs.discard_keep)}
              </AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={commitViewChange}
              >
                {t(($) => $.tabs.discard_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </div>
  );
}

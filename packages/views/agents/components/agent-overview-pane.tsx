"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  BookOpenText,
  FileText,
  KeyRound,
  ListTodo,
  Plug,
  Router,
  Terminal,
  Webhook,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { providerSupportsMcpConfig } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { larkInstallationsOptions } from "@multica/core/lark";
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
import { ActivityTab } from "./tabs/activity-tab";
import { InstructionsTab } from "./tabs/instructions-tab";
import { SkillsTab } from "./tabs/skills-tab";
import { EnvTab } from "./tabs/env-tab";
import { CustomArgsTab } from "./tabs/custom-args-tab";
import { McpConfigTab } from "./tabs/mcp-config-tab";
import { IntegrationsTab } from "./tabs/integrations-tab";
import { RuntimeConfigTab } from "./tabs/runtime-config-tab";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { useT } from "../../i18n";

export type DetailTab =
  | "activity"
  | "tasks"
  | "instructions"
  | "skills"
  | "env"
  | "custom_args"
  | "mcp_config"
  | "integrations"
  | "runtime_config";

const TAB_LABEL_KEY: Record<DetailTab, "activity" | "tasks" | "instructions" | "skills" | "environment" | "custom_args" | "mcp_config" | "integrations" | "runtime_config"> = {
  activity: "activity",
  tasks: "tasks",
  instructions: "instructions",
  skills: "skills",
  env: "environment",
  custom_args: "custom_args",
  mcp_config: "mcp_config",
  integrations: "integrations",
  runtime_config: "runtime_config",
};

const detailTabs: {
  id: DetailTab;
  icon: typeof FileText;
}[] = [
  { id: "activity", icon: Activity },
  { id: "tasks", icon: ListTodo },
  { id: "instructions", icon: FileText },
  { id: "skills", icon: BookOpenText },
  { id: "env", icon: KeyRound },
  { id: "custom_args", icon: Terminal },
  { id: "mcp_config", icon: Plug },
  { id: "integrations", icon: Webhook },
  { id: "runtime_config", icon: Router },
];

interface AgentOverviewPaneProps {
  agent: Agent;
  runtimes: AgentRuntime[];
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  /**
   * One-shot request from a sibling (the inspector's compact Lark status
   * row) to focus a specific tab. Routed through the same `requestTabChange`
   * the tab buttons use, so the unsaved-changes guard still fires. The pane
   * calls `onNavIntentHandled` to clear it after consuming.
   */
  navIntent?: DetailTab | null;
  onNavIntentHandled?: () => void;
}

/**
 * Right-pane on the agent detail page:
 *
 *   - Activity (default) — what the agent is doing now / how it's been doing /
 *     what it just finished. The "watch state" surface.
 *   - Tasks — assigned/created issues using the shared issue board/list.
 *   - Instructions / Skills / Env / Custom Args — four editing surfaces.
 *
 * The previous Settings tab was deleted because every field on it is now
 * inline-editable in the inspector (left column) — runtime / model /
 * visibility / concurrency via PropRow + Picker, and avatar / name /
 * description via popover. Two entry points for the same writes was just
 * extra concept count without extra capability.
 *
 * Activity is the landing tab because most visits to this page are diagnostic
 * ("what is this agent doing / why did it fail?"), not configuration tweaks.
 *
 * **Unsaved-changes guard**: every config tab reports its dirty state up via
 * `onDirtyChange`. Switching to another tab while the active tab is dirty
 * pops a confirm dialog — without it, switching tabs would silently drop
 * unsaved edits because each tab manages its own local state and remounts on
 * tab change.
 */
export function AgentOverviewPane({
  agent,
  runtimes,
  onUpdate,
  navIntent,
  onNavIntentHandled,
}: AgentOverviewPaneProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const [activeTab, setActiveTab] = useState<DetailTab>("activity");
  const [activeDirty, setActiveDirty] = useState(false);
  // Holds the destination when a tab change is intercepted by the dirty
  // guard. Null means no pending change. The AlertDialog reads non-null as
  // "open".
  const [pendingTab, setPendingTab] = useState<DetailTab | null>(null);

  const runtime = agent.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;

  // Cached per-workspace and shared with the inspector's bind button, so this
  // is at most one extra GET per workspace. We only read `configured` to
  // decide whether the Integrations tab is worth showing at all.
  const { data: larkListing } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const larkConfigured = larkListing?.configured === true;

  // The MCP tab is only shown when the agent's runtime backend actually
  // consumes mcp_config — see providerSupportsMcpConfig. We default to
  // showing it when the runtime row hasn't loaded yet so a slow fetch
  // can't transiently flicker the tab off and then on.
  //
  // The Integrations tab only appears once the deployment has Lark wired
  // (configured). Unlike MCP we default to HIDING while the listing loads:
  // deployments without Lark are the common case, so flashing the tab on
  // then off would be the worse flicker.
  //
  // The Runtime Config tab is openclaw-only today (gateway mode lives there,
  // issue #3260). Other providers' runtime_config is freeform JSONB that no
  // backend currently reads, so surfacing the tab would let users save values
  // their runtime ignores — same anti-footgun rationale as the MCP gate.
  const visibleTabs = useMemo(() => {
    const showMcp = runtime ? providerSupportsMcpConfig(runtime.provider) : true;
    const showRuntimeConfig = runtime ? runtime.provider === "openclaw" : false;
    return detailTabs.filter((tab) => {
      if (tab.id === "mcp_config") return showMcp;
      if (tab.id === "integrations") return larkConfigured;
      if (tab.id === "runtime_config") return showRuntimeConfig;
      return true;
    });
  }, [runtime, larkConfigured]);

  // If the active tab disappears (e.g. user just switched the agent's
  // runtime to one that doesn't read mcp_config), fall back to Activity
  // for this render so the pane is never empty. The user's stored
  // activeTab is left alone — switching back to a supporting runtime
  // brings their selection back.
  const effectiveTab: DetailTab = visibleTabs.some((tab) => tab.id === activeTab)
    ? activeTab
    : "activity";

  const requestTabChange = (next: DetailTab) => {
    if (next === activeTab) return;
    if (activeDirty) {
      setPendingTab(next);
      return;
    }
    setActiveTab(next);
  };

  const commitTabChange = () => {
    if (pendingTab) {
      setActiveTab(pendingTab);
      // The new tab mounts fresh; its effect will report its own dirty state.
      // We pre-clear so the guard can't trip from stale state on the way in.
      setActiveDirty(false);
      setPendingTab(null);
    }
  };

  // Consume a one-shot tab-focus request from a sibling. Routing through
  // `requestTabChange` (rather than `setActiveTab`) keeps the unsaved-changes
  // guard honored even when the request originates outside the tab strip. The
  // effect body is a no-op while `navIntent` is null, so the unstable
  // `requestTabChange`/`onNavIntentHandled` identities can't loop it.
  useEffect(() => {
    if (navIntent == null) return;
    requestTabChange(navIntent);
    onNavIntentHandled?.();
  }, [navIntent, requestTabChange, onNavIntentHandled]);

  return (
    // On mobile the parent stacks the inspector and overview and scrolls the
    // page itself, so this pane has no inherited height. `min-h-[60vh]` keeps
    // the tab content area usably tall when content is short; `md:` restores
    // the grid-driven full-height behavior on tablet and up.
    <div className="flex min-h-[60vh] flex-col overflow-hidden rounded-lg border bg-background md:h-full md:min-h-0">
      <div className="flex shrink-0 items-center gap-0 overflow-x-auto border-b px-2 md:px-4">
        {visibleTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => requestTabChange(tab.id)}
            className={`flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2.5 text-xs font-medium transition-colors ${
              effectiveTab === tab.id
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <tab.icon className="h-3.5 w-3.5" />
            {t(($) => $.tabs[TAB_LABEL_KEY[tab.id]])}
          </button>
        ))}
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto">
        {effectiveTab === "activity" && <ActivityTab agent={agent} />}
        {effectiveTab === "tasks" && (
          <div className="flex h-full min-h-[520px] flex-col">
            <ActorIssuesPanel actorType="agent" actorId={agent.id} />
          </div>
        )}
        {effectiveTab === "instructions" && (
          <TabContent>
            <InstructionsTab
              agent={agent}
              onSave={(instructions) => onUpdate(agent.id, { instructions })}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {effectiveTab === "skills" && (
          <TabContent>
            <SkillsTab agent={agent} />
          </TabContent>
        )}
        {effectiveTab === "env" && (
          <TabContent>
            <EnvTab
              agent={agent}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {effectiveTab === "custom_args" && (
          <TabContent>
            <CustomArgsTab
              agent={agent}
              runtimeDevice={runtime ?? undefined}
              onSave={(updates) => onUpdate(agent.id, updates)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {effectiveTab === "mcp_config" && (
          <TabContent>
            <McpConfigTab
              agent={agent}
              onSave={(updates) => onUpdate(agent.id, updates)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {effectiveTab === "integrations" && (
          <TabContent>
            <IntegrationsTab agent={agent} />
          </TabContent>
        )}
        {effectiveTab === "runtime_config" && (
          <TabContent>
            <RuntimeConfigTab
              agent={agent}
              onSave={(updates) => onUpdate(agent.id, updates)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
      </div>

      {pendingTab !== null && (
        <AlertDialog
          open
          onOpenChange={(v) => {
            if (!v) setPendingTab(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.tabs.discard_dialog_title)}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.tabs.discard_dialog_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t(($) => $.tabs.discard_keep)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={commitTabChange}
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

// Padded, full-width container shared by every config tab. `h-full flex
// flex-col` lets a tab opt into "fill the viewport" by giving its root
// element `flex-1 min-h-0` (Instructions does this so the editor expands
// instead of pushing the Save row off-screen). Tabs that don't opt in
// behave as natural-height blocks; long content (e.g. Settings, long Skills
// list) still scrolls via the parent's overflow-y-auto.
function TabContent({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col p-4 md:p-6">{children}</div>
  );
}

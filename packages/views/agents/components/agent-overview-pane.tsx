"use client";

import { useState } from "react";
import {
  Activity,
  BookOpenText,
  FileText,
  KeyRound,
  Terminal,
} from "lucide-react";
import { useT } from "@multica/i18n/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
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

type DetailTab =
  | "activity"
  | "instructions"
  | "skills"
  | "env"
  | "custom_args";

interface AgentOverviewPaneProps {
  agent: Agent;
  runtimes: AgentRuntime[];
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}

/**
 * Right-pane on the agent detail page. Five tabs of equal weight:
 *
 *   - Activity (default) — what the agent is doing now / how it's been doing /
 *     what it just finished. The "watch state" surface.
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
}: AgentOverviewPaneProps) {
  const t = useT("agents");

  const detailTabs: {
    id: DetailTab;
    label: string;
    icon: typeof FileText;
  }[] = [
    { id: "activity", label: t("overview_activity"), icon: Activity },
    { id: "instructions", label: t("overview_instructions"), icon: FileText },
    { id: "skills", label: t("overview_skills"), icon: BookOpenText },
    { id: "env", label: t("overview_environment"), icon: KeyRound },
    { id: "custom_args", label: t("overview_custom_args"), icon: Terminal },
  ];

  const [activeTab, setActiveTab] = useState<DetailTab>("activity");
  const [activeDirty, setActiveDirty] = useState(false);
  // Holds the destination when a tab change is intercepted by the dirty
  // guard. Null means no pending change. The AlertDialog reads non-null as
  // "open".
  const [pendingTab, setPendingTab] = useState<DetailTab | null>(null);

  const runtime = agent.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;

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

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border bg-background">
      <div className="flex shrink-0 items-center gap-0 border-b px-4">
        {detailTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => requestTabChange(tab.id)}
            className={`flex items-center gap-1.5 border-b-2 px-3 py-2.5 text-xs font-medium transition-colors ${
              activeTab === tab.id
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <tab.icon className="h-3.5 w-3.5" />
            {tab.label}
          </button>
        ))}
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto">
        {activeTab === "activity" && <ActivityTab agent={agent} />}
        {activeTab === "instructions" && (
          <TabContent>
            <InstructionsTab
              agent={agent}
              onSave={(instructions) => onUpdate(agent.id, { instructions })}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {activeTab === "skills" && (
          <TabContent>
            <SkillsTab agent={agent} />
          </TabContent>
        )}
        {activeTab === "env" && (
          <TabContent>
            <EnvTab
              agent={agent}
              readOnly={agent.custom_env_redacted}
              onSave={(updates) => onUpdate(agent.id, updates)}
              onDirtyChange={setActiveDirty}
            />
          </TabContent>
        )}
        {activeTab === "custom_args" && (
          <TabContent>
            <CustomArgsTab
              agent={agent}
              runtimeDevice={runtime ?? undefined}
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
              <AlertDialogTitle>{t("overview_discard_title")}</AlertDialogTitle>
              <AlertDialogDescription>
                {t("overview_discard_desc")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("overview_keep_editing")}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={commitTabChange}
              >
                {t("overview_discard_changes")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </div>
  );
}

// Centred, max-width container shared by every config tab. `h-full flex
// flex-col` lets a tab opt into "fill the viewport" by giving its root
// element `flex-1 min-h-0` (Instructions does this so the editor expands
// instead of pushing the Save row off-screen). Tabs that don't opt in
// behave as natural-height blocks; long content (e.g. Settings, long Skills
// list) still scrolls via the parent's overflow-y-auto.
function TabContent({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto flex h-full max-w-2xl flex-col p-6">{children}</div>
  );
}

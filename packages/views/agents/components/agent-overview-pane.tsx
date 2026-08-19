"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Users } from "lucide-react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  Squad,
  SquadMember,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  squadListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
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
import { buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  AGENT_ROLE_CENTER_TABS,
  type AgentRoleCenterTab,
} from "../role-center-model";
import { AppLink, useNavigation } from "../../navigation";
import { AgentDetailInspector } from "./agent-detail-inspector";
import { AgentOverviewSummary } from "./agent-overview-summary";
import { SkillsTab } from "./tabs/skills-tab";
import { InstructionsTab } from "./tabs/instructions-tab";
import { useT } from "../../i18n";

export type DetailTab = AgentRoleCenterTab;

const TAB_LABELS: Record<AgentRoleCenterTab, "overview" | "skills" | "instructions" | "general"> = {
  overview: "overview",
  skills: "skills",
  instructions: "instructions",
  general: "general",
};

function isRoleCenterTab(value: string | null): value is AgentRoleCenterTab {
  return (
    value !== null &&
    (AGENT_ROLE_CENTER_TABS as readonly string[]).includes(value)
  );
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
 * Tag keeps Agents deliberately narrow: it is the place to understand and
 * configure a role. Conversations live in Chat and task history lives in
 * Tasks, so neither becomes a competing second workbench here.
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
  const navigation = useNavigation();
  const urlView = navigation.searchParams.get("view");
  const [activeView, setActiveView] = useState<AgentRoleCenterTab>(() =>
    isRoleCenterTab(urlView) ? urlView : "overview",
  );
  const [activeDirty, setActiveDirty] = useState(false);
  const [pendingView, setPendingView] = useState<AgentRoleCenterTab | null>(
    null,
  );
  const lastUrlViewRef = useRef(urlView);

  const commitView = useCallback(
    (next: AgentRoleCenterTab) => {
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
    (next: AgentRoleCenterTab) => {
      if (next === activeView) return;
      if (activeDirty) {
        setPendingView(next);
        return;
      }
      commitView(next);
    },
    [activeDirty, activeView, commitView],
  );

  useEffect(() => {
    if (urlView === lastUrlViewRef.current) return;
    lastUrlViewRef.current = urlView;
    setActiveView(isRoleCenterTab(urlView) ? urlView : "overview");
  }, [urlView]);

  useEffect(() => {
    if (navIntent == null) return;
    requestView(navIntent);
    onNavIntentHandled?.();
  }, [navIntent, onNavIntentHandled, requestView]);

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <div
        className="shrink-0 overflow-x-auto border-b px-4 sm:px-6"
        role="tablist"
        aria-label={t(($) => $.tabs.page_navigation_aria)}
      >
        <div className="mx-auto flex max-w-[1440px] items-center gap-6">
          {AGENT_ROLE_CENTER_TABS.map((tab) => (
            <button
              key={tab}
              type="button"
              role="tab"
              aria-selected={activeView === tab}
              onClick={() => requestView(tab)}
              className={cn(
                "relative shrink-0 py-3 text-body font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                activeView === tab
                  ? "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t(($) => $.tabs[TAB_LABELS[tab]])}
            </button>
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {activeView === "overview" && (
          <RoleCenterOverview agent={agent} runtime={runtime} owner={owner} />
        )}
        {activeView === "skills" && (
          <div className="mx-auto w-full max-w-3xl p-4 sm:p-6 md:p-8">
            <SkillsTab agent={agent} runtime={runtime} canEdit={canEdit} />
          </div>
        )}
        {activeView === "instructions" && (
          <div className="mx-auto w-full max-w-3xl p-4 sm:p-6 md:p-8">
            <InstructionsTab
              agent={agent}
              onSave={(instructions) => onUpdate(agent.id, { instructions })}
              onDirtyChange={setActiveDirty}
            />
          </div>
        )}
        {activeView === "general" && (
          <div className="mx-auto w-full max-w-3xl p-4 sm:p-6 md:p-8">
            <AgentDetailInspector
              agent={agent}
              runtime={runtime}
              runtimes={runtimes}
              members={members}
              currentUserId={currentUserId ?? null}
              canEdit={canEdit}
              onUpdate={onUpdate}
            />
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
                onClick={() => {
                  commitView(pendingView);
                  setActiveDirty(false);
                  setPendingView(null);
                }}
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

function RoleCenterOverview({
  agent,
  runtime,
  owner,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
}) {
  const { t } = useT("agents");
  const paths = useWorkspacePaths();

  return (
    <div className="mx-auto max-w-[1440px] p-4 sm:p-6">
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_320px]">
        <section className="self-start rounded-xl border border-surface-border bg-surface p-5 shadow-[var(--surface-shadow)]">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 className="text-body font-medium">
                {t(($) => $.tabs.tasks)}
              </h2>
              <p className="mt-1 max-w-xl text-caption leading-5 text-muted-foreground">
                {t(($) => $.role_center.tasks_description)}
              </p>
            </div>
            <AppLink
              href={paths.issues()}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              {t(($) => $.tabs.tasks)}
              <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
            </AppLink>
          </div>
          <AgentTeamMemberships agentId={agent.id} />
        </section>
        <AgentOverviewSummary agent={agent} runtime={runtime} owner={owner} />
      </div>
    </div>
  );
}

function AgentTeamMemberships({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const activeSquads = useMemo(
    () => squads.filter((squad) => !squad.archived_at),
    [squads],
  );
  const memberQueries = useQueries({
    queries: activeSquads.map((squad) => ({
      queryKey: [...workspaceKeys.squads(wsId), squad.id, "members"],
      queryFn: () => api.listSquadMembers(squad.id),
      enabled: !!wsId,
    })),
  });
  const teams = useMemo(
    () =>
      activeSquads.filter((_squad, index) =>
        (memberQueries[index]?.data as SquadMember[] | undefined)?.some(
          (member) =>
            member.member_type === "agent" && member.member_id === agentId,
        ),
      ),
    [activeSquads, agentId, memberQueries],
  );

  return (
    <div className="mt-6 border-t pt-5">
      <div className="flex items-center gap-2 text-body font-medium">
        <Users className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
        {t(($) => $.role_center.team_membership)}
      </div>
      {teams.length > 0 ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {teams.map((team: Squad) => (
            <AppLink
              key={team.id}
              href={paths.squadDetail(team.id)}
              className="rounded-md border border-surface-border bg-surface-hover px-2 py-1 text-caption text-foreground"
            >
              {team.name}
            </AppLink>
          ))}
        </div>
      ) : (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.role_center.no_team)}
        </p>
      )}
    </div>
  );
}

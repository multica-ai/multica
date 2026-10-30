"use client";

import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, Link2, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { DropdownMenuItem, DropdownMenuSeparator } from "@multica/ui/components/ui/dropdown-menu";
import { api } from "@multica/core/api";
import { isAgentRuntimeBound, useWorkspacePresenceMap } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAgentPermissions } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes";
import { agentListOptions, memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { AgentOverviewPane } from "../../agents/components/agent-overview-pane";
import { useAgentUpdate } from "../../agents/components/use-agent-update";
import { useT } from "../../i18n";
import { TEAM_TABS, teamHref, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamAgentChat, TeamAgentThreadPane, useAgentConversation } from "./team-agent-chat";
import { TeamAgentPanel } from "./team-agent-panel";
import { TeamNotFound } from "./team-not-found";
import { AgentStatusPill, useTeamCopy } from "./team-ui";

export function TeamAgentView({
  agentId,
  tab,
  sessionId,
  threadId,
}: {
  agentId: string;
  tab: TeamTab;
  sessionId: string | null;
  threadId: string | null;
}) {
  const { t } = useT("team");
  const { t: tAgents } = useT("agents");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const agent = agents.find((a) => a.id === agentId) ?? null;
  const { canAssign, canEdit } = useAgentPermissions(agent, wsId);
  const onUpdate = useAgentUpdate();
  const { copyLink } = useTeamCopy();
  const [panelOpen, setPanelOpen] = useState(true);
  const [confirmArchive, setConfirmArchive] = useState(false);

  if (!agent) {
    return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;
  }

  const runtime = isAgentRuntimeBound(agent)
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner = agent.owner_id ? members.find((m) => m.user_id === agent.owner_id) ?? null : null;
  const presence = presenceMap.get(agent.id) ?? null;
  const hasPanel = tab === "chat" || tab === "work";
  const canArchive = canEdit.allowed && !agent.archived_at && !agent.system_key;

  const assign = () => {
    if (!isAgentRuntimeBound(agent)) {
      toast.error(tAgents(($) => $.detail.runtime_required_toast));
      return;
    }
    useModalStore.getState().open("quick-create-issue", { agent_id: agent.id });
  };

  const archive = async () => {
    try {
      await api.archiveAgent(agent.id);
      void qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(tAgents(($) => $.row_actions.agent_archived_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tAgents(($) => $.row_actions.archive_failed_toast));
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="agent"
        id={agent.id}
        tabs={TEAM_TABS.agent}
        activeTab={tab}
        icon={<ActorAvatar actorType="agent" actorId={agent.id} size="md" showStatusDot />}
        name={agent.name}
        menu={
          <>
            <DropdownMenuItem onClick={() => copyLink(teamHref(wsPaths.team(), { kind: "agent", id: agent.id }))}>
              <Link2 className="size-3.5" />
              {t(($) => $.detail.copy_link)}
            </DropdownMenuItem>
            {canArchive && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={() => setConfirmArchive(true)}>
                  <Archive className="size-3.5" />
                  {t(($) => $.agent.archive)}
                </DropdownMenuItem>
              </>
            )}
          </>
        }
        status={<AgentStatusPill presence={presence} archived={!!agent.archived_at} />}
        actions={
          canAssign.allowed && !agent.archived_at ? (
            <Button size="sm" variant="outline" onClick={assign}>
              <Plus className="size-3.5" />
              {t(($) => $.agent.assign_issue)}
            </Button>
          ) : null
        }
        panelOpen={hasPanel ? panelOpen : undefined}
        onTogglePanel={hasPanel ? () => setPanelOpen((v) => !v) : undefined}
      />
      <div className="flex min-h-0 flex-1">
        {tab === "chat" ? (
          <TeamAgentChatArea
            agent={agent}
            sessionId={sessionId}
            threadId={threadId}
            panel={
              panelOpen ? (
                <TeamAgentPanel agent={agent} runtime={runtime} owner={owner} presence={presence} openSessionId={sessionId} />
              ) : null
            }
          />
        ) : (
          <>
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              {tab === "work" && <ActorIssuesPanel actorType="agent" actorId={agent.id} />}
              {tab === "setup" && (
                <AgentOverviewPane
                  variant="setup"
                  agent={agent}
                  runtime={runtime}
                  owner={owner}
                  runtimes={runtimes}
                  members={members}
                  onUpdate={onUpdate}
                  currentUserId={user?.id ?? null}
                  canEdit={canEdit.allowed}
                />
              )}
            </div>
            {tab === "work" && panelOpen && (
              <TeamAgentPanel agent={agent} runtime={runtime} owner={owner} presence={presence} openSessionId={null} />
            )}
          </>
        )}
      </div>
      {confirmArchive && (
        <AlertDialog open onOpenChange={(v) => !v && setConfirmArchive(false)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle className="break-all">
                {tAgents(($) => $.row_actions.archive_dialog_title, { name: agent.name })}
              </AlertDialogTitle>
              <AlertDialogDescription>{tAgents(($) => $.row_actions.archive_dialog_description)}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{tAgents(($) => $.row_actions.archive_dialog_cancel)}</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirmArchive(false);
                  void archive();
                }}
              >
                {tAgents(($) => $.row_actions.archive_dialog_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </div>
  );
}

/** The conversation, and beside it the open thread — or the agent's panel. */
function TeamAgentChatArea({
  agent,
  sessionId,
  threadId,
  panel,
}: {
  agent: Agent;
  sessionId: string | null;
  threadId: string | null;
  panel: ReactNode;
}) {
  const wsPaths = useWorkspacePaths();
  const { thread, earlier } = useAgentConversation(agent, sessionId, threadId);
  return (
    <>
      <TeamAgentChat agent={agent} sessionId={sessionId} threadId={threadId} />
      {thread ? (
        <TeamAgentThreadPane
          agent={agent}
          thread={thread}
          readOnly={!!earlier || !!agent.archived_at}
          closeHref={teamHref(wsPaths.team(), { kind: "agent", id: agent.id, sessionId: earlier?.id })}
        />
      ) : (
        panel
      )}
    </>
  );
}

export function TeamDetailSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-12 items-center gap-2 border-b px-4">
        <Skeleton className="size-6 rounded-full" />
        <Skeleton className="h-4 w-40" />
      </div>
      <div className="flex-1 space-y-3 p-6">
        <Skeleton className="h-4 w-2/3" />
        <Skeleton className="h-4 w-1/2" />
      </div>
    </div>
  );
}

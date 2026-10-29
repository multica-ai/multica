"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { PanelRight, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { isAgentRuntimeBound, useWorkspacePresenceMap } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useAgentPermissions } from "@multica/core/permissions";
import { runtimeListOptions } from "@multica/core/runtimes";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { AgentOverviewPane } from "../../agents/components/agent-overview-pane";
import { AgentPresenceIndicator } from "../../agents/components/agent-presence-indicator";
import { useAgentUpdate } from "../../agents/components/use-agent-update";
import { useT } from "../../i18n";
import { TEAM_TABS, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamAgentChat } from "./team-agent-chat";
import { TeamAgentPanel } from "./team-agent-panel";
import { TeamNotFound } from "./team-not-found";

export function TeamAgentView({
  agentId,
  tab,
  sessionId,
}: {
  agentId: string;
  tab: TeamTab;
  sessionId: string | null;
}) {
  const { t } = useT("team");
  const { t: tAgents } = useT("agents");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: agents = [], isLoading } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const agent = agents.find((a) => a.id === agentId) ?? null;
  const { canAssign, canEdit } = useAgentPermissions(agent, wsId);
  const onUpdate = useAgentUpdate();
  const [panelOpen, setPanelOpen] = useState(true);

  if (!agent) {
    return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;
  }

  const runtime = isAgentRuntimeBound(agent)
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const owner = agent.owner_id ? members.find((m) => m.user_id === agent.owner_id) ?? null : null;
  const presence = presenceMap.get(agent.id) ?? null;
  const showPanelToggle = tab === "chat" || tab === "work";

  const assign = () => {
    if (!isAgentRuntimeBound(agent)) {
      toast.error(tAgents(($) => $.detail.runtime_required_toast));
      return;
    }
    useModalStore.getState().open("quick-create-issue", { agent_id: agent.id });
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="agent"
        id={agent.id}
        tabs={TEAM_TABS.agent}
        activeTab={tab}
        identity={
          <>
            <ActorAvatar actorType="agent" actorId={agent.id} size="md" showStatusDot />
            <h2 className="truncate text-body font-medium">{agent.name}</h2>
            {!agent.archived_at && <AgentPresenceIndicator detail={presence} compact />}
          </>
        }
        actions={
          <>
            {canAssign.allowed && !agent.archived_at && (
              <Button size="sm" variant="outline" onClick={assign}>
                <Plus className="size-3.5" />
                {t(($) => $.agent.assign_issue)}
              </Button>
            )}
            {showPanelToggle && (
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={t(($) => $.detail.toggle_panel)}
                aria-pressed={panelOpen}
                onClick={() => setPanelOpen((v) => !v)}
                className={cn(panelOpen && "bg-surface-selected")}
              >
                <PanelRight className="size-4" />
              </Button>
            )}
          </>
        }
      />
      <div className="flex min-h-0 flex-1">
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {tab === "chat" && <TeamAgentChat agent={agent} sessionId={sessionId} />}
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
        {showPanelToggle && panelOpen && (
          <TeamAgentPanel
            agent={agent}
            runtime={runtime}
            owner={owner}
            presence={presence}
            openSessionId={tab === "chat" ? sessionId : null}
          />
        )}
      </div>
    </div>
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

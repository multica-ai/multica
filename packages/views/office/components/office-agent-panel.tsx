"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Building2, ChevronDown, ListTodo } from "@multica/ui/icons";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { IssueDetail } from "../../issues/components/issue-detail";
import { OfficeAgentChat } from "./office-agent-chat";
import { OfficeAgentNewIssue } from "./office-agent-new-issue";
import { OfficeAgentWork } from "./office-agent-work";
import { OfficeHomeSquadMenu } from "./office-home-squad-menu";

/** What the right-hand side shows: the agent's chat, one issue's thread, or the new-issue form. */
type Pane = { kind: "chat" } | { kind: "issue"; issueId: string } | { kind: "new" };

/** Which side shows at narrow widths, where the two do not fit side by side. */
type NarrowSide = "work" | "main";

export interface OfficeAgentTarget {
  agentId: string;
  /** Opens on this issue's thread; the agent's chat when absent. */
  issueId?: string;
}

/**
 * The one place an agent is managed from. It opens from a click on the agent or on
 * its status tag, and holds everything an admin does with one agent: change its
 * home squad, see and hand over its work, create issues for it, talk to it, and
 * talk to it inside an issue.
 */
export function OfficeAgentPanel({
  target,
  issues,
  projectId,
  onClose,
}: {
  /** The agent to manage; the panel is closed while this is null. */
  target: OfficeAgentTarget | null;
  issues: readonly Issue[];
  projectId?: string;
  onClose: () => void;
}) {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agent = target ? agents.find((candidate) => candidate.id === target.agentId) : undefined;
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex h-[min(88dvh,54rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        {target && agent ? (
          // A new key starts the panel afresh for each agent and each way of opening it.
          <PanelBody
            key={`${target.agentId}:${target.issueId ?? ""}`}
            agent={agent}
            initialIssueId={target.issueId}
            issues={issues}
            projectId={projectId}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function PanelBody({
  agent,
  initialIssueId,
  issues,
  projectId,
}: {
  agent: Agent;
  initialIssueId?: string;
  issues: readonly Issue[];
  projectId?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const homeSquad = squads.find((squad) => squad.id === agent.home_squad_id && !squad.archived_at);
  const [pane, setPane] = useState<Pane>(initialIssueId ? { kind: "issue", issueId: initialIssueId } : { kind: "chat" });
  const [side, setSide] = useState<NarrowSide>("main");

  const show = (next: Pane) => {
    setPane(next);
    setSide("main");
  };
  const backToChat = () => show({ kind: "chat" });

  return (
    <>
      <header className="flex shrink-0 flex-wrap items-center gap-3 border-b px-4 py-3 pr-12">
        <ActorAvatar actorType="agent" actorId={agent.id} size="lg" showStatusDot />
        <DialogTitle className="min-w-0 flex-1 truncate text-heading">{agent.name}</DialogTitle>
        <Button type="button" variant="outline" size="sm" className="md:hidden" onClick={() => setSide(side === "work" ? "main" : "work")}>
          <ListTodo className="size-4" />
          {t(($) => $.office.panel_work)}
        </Button>
        <OfficeHomeSquadMenu
          agent={agent}
          trigger={
            <Button type="button" variant="outline" size="sm" aria-label={t(($) => $.office.home_squad)}>
              <Building2 className="size-4" />
              {homeSquad?.name ?? t(($) => $.office.home_squad_none)}
              <ChevronDown className="size-3.5 text-muted-foreground" />
            </Button>
          }
        />
      </header>
      <div className="flex min-h-0 flex-1">
        <aside
          aria-label={t(($) => $.office.panel_work)}
          className={cn("min-h-0 w-full flex-col border-r md:flex md:w-80 md:shrink-0", side === "work" ? "flex" : "hidden")}
        >
          <OfficeAgentWork
            agentId={agent.id}
            issues={issues}
            selectedIssueId={pane.kind === "issue" ? pane.issueId : null}
            onSelect={(issueId) => show({ kind: "issue", issueId })}
            onNew={() => show({ kind: "new" })}
          />
        </aside>
        <section className={cn("min-h-0 min-w-0 flex-1 flex-col md:flex", side === "main" ? "flex" : "hidden")}>
          {/* The chat stays mounted behind the other panes, so coming back finds the same conversation. */}
          <div className={cn("min-h-0 flex-1 flex-col", pane.kind === "chat" ? "flex" : "hidden")}>
            <OfficeAgentChat agent={agent} projectId={projectId} active={pane.kind === "chat"} />
          </div>
          {pane.kind === "new" ? (
            <OfficeAgentNewIssue
              agent={agent}
              projectId={projectId}
              onCreated={(issueId) => show({ kind: "issue", issueId })}
              onCancel={backToChat}
            />
          ) : null}
          {pane.kind === "issue" ? (
            <IssueDetail
              key={pane.issueId}
              issueId={pane.issueId}
              variant="peek"
              defaultSidebarOpen={false}
              leadingAction={
                <Button type="button" variant="ghost" size="sm" onClick={backToChat}>
                  <ArrowLeft className="size-4" />
                  {t(($) => $.office.issue_back)}
                </Button>
              }
              onDelete={backToChat}
            />
          ) : null}
        </section>
      </div>
    </>
  );
}

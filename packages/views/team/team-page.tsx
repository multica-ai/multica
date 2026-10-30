"use client";

import { ArrowLeft, MessagesSquare } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { useListColumnCollapsed } from "../layout/app-shell-context";
import { ListColumn } from "../layout/list-column";
import { parseTeamSelection, teamHref } from "./selection";
import { TeamSidebar } from "./components/team-sidebar";
import { TeamAgentView } from "./components/team-agent-view";
import { TeamSquadView } from "./components/team-squad-view";
import { TeamMemberView } from "./components/team-member-view";
import { TeamChannelView } from "./components/team-channel-view";

/**
 * Team: one place for everyone who works in the workspace — agents, squads
 * and members. The left list picks who; the right pane is that object's
 * Chat / Work / Setup. Selection is URL state (see `selection.ts`).
 */
export function TeamPage() {
  const { t } = useT("team");
  const { searchParams, push } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const isCompact = useIsCompact();
  const selection = parseTeamSelection(searchParams);
  const listCollapsed = useListColumnCollapsed();

  const detail =
    selection.kind === "agent" ? (
      <TeamAgentView
        key={`agent:${selection.id}`}
        agentId={selection.id}
        tab={selection.tab}
        sessionId={selection.sessionId}
        threadId={selection.threadId}
      />
    ) : selection.kind === "squad" ? (
      <TeamSquadView key={`squad:${selection.id}`} squadId={selection.id} tab={selection.tab} />
    ) : selection.kind === "member" ? (
      <TeamMemberView
        key={`member:${selection.id}`}
        userId={selection.id}
        tab={selection.tab}
        threadId={selection.threadId}
      />
    ) : selection.kind === "channel" ? (
      <TeamChannelView key={`channel:${selection.id}`} channelId={selection.id} threadId={selection.threadId} />
    ) : (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
        <MessagesSquare className="h-10 w-10 text-faint-foreground" />
        <p className="text-body">{t(($) => $.page.select_prompt)}</p>
      </div>
    );

  if (isCompact) {
    if (selection.kind !== "none") {
      return (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => push(teamHref(wsPaths.team(), { kind: "none" }))}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              {t(($) => $.page.title)}
            </Button>
          </div>
          {detail}
        </div>
      );
    }
    return <TeamSidebar selection={selection} />;
  }

  return (
    <div className="flex min-h-0 flex-1">
      <ListColumn collapsed={listCollapsed}>
        <TeamSidebar selection={selection} />
      </ListColumn>
      <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">{detail}</div>
    </div>
  );
}

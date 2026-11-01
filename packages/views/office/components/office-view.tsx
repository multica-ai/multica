"use client";

import { memo, useCallback, useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { layoutFloor, officeFloorsOptions, officeSquadMembersOptions, useOfficeFloorStore } from "@multica/core/office";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import type { Agent, AgentTask, IssueStatus, Squad, SquadMember } from "@multica/core/types";
import type { OfficeFloor } from "@multica/core/office";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { BoardView, type BoardViewProps } from "../../issues/components/board-view";
import { useStatusLabel } from "../../issues/utils/status-label";
import { focusIssues, officeTagOf } from "../lib/office-tag";
import { OfficeAgentPanel, type OfficeAgentTarget } from "./office-agent-panel";
import { OfficeCanvas, type WorkerSession } from "./office-canvas";
import { OfficeBoardButton } from "./office-board-button";
import { OfficeBoardDialog } from "./office-board-dialog";
import { OfficeRoomOverlays } from "./office-room-overlays";

const EMPTY_TASKS: AgentTask[] = [];
const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];
const EMPTY_FLOORS: OfficeFloor[] = [];
const EMPTY_SET: ReadonlySet<string> = new Set();

/**
 * The company, drawn as the pixtuoid office. A floor holds six squad rooms; each
 * agent sits at a desk of its home squad's room, whether or not it has an issue.
 * The tag over a worker names its focus issue's status. A click on the worker or
 * on its tag opens the one panel that manages the agent: its home squad, its
 * work, new issues and chat. Any other grouping falls back to the classic board.
 */
export const OfficeView = memo(function OfficeView(props: BoardViewProps) {
  const grouping = useViewStore((s) => s.grouping);
  const setGrouping = useViewStore((s) => s.setGrouping);
  const { t } = useT("issues");

  if (grouping !== "status") {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="mx-4 mb-2 flex shrink-0 items-center gap-2 rounded-lg bg-muted/50 px-3 py-1.5 text-caption text-muted-foreground">
          <span className="min-w-0 flex-1">{t(($) => $.office.grouping_notice)}</span>
          <Button type="button" variant="outline" size="xs" onClick={() => setGrouping("status")}>
            {t(($) => $.office.grouping_reset)}
          </Button>
        </div>
        <BoardView {...props} />
      </div>
    );
  }
  return <OfficeScene {...props} />;
});

function OfficeScene({ issues, visibleStatuses, projectId }: BoardViewProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const labelOf = useStatusLabel(wsId);
  const { data: tasks = EMPTY_TASKS } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: agents = EMPTY_AGENTS } = useQuery(agentListOptions(wsId));
  const { data: squads = EMPTY_SQUADS } = useQuery(squadListOptions(wsId));
  const { data: floors = EMPTY_FLOORS } = useQuery(officeFloorsOptions(wsId));
  const lastFloorId = useOfficeFloorStore((s) => s.lastFloorId);

  // The floor on show: the one last viewed, else the first.
  const floor = floors.find((candidate) => candidate.id === lastFloorId) ?? floors[0] ?? null;

  // Members in join order seat the desks (leader first), for the squads on this floor.
  const floorSquadIds = useMemo(
    () => squads.filter((squad) => !squad.archived_at && !!floor && squad.floor_id === floor.id).map((squad) => squad.id),
    [squads, floor],
  );
  const membersBySquad = useQueries({
    queries: floorSquadIds.map((id) => officeSquadMembersOptions(wsId, id)),
    combine: (results) => results.map((result) => result.data),
  });
  const layout = useMemo(() => {
    const members: Record<string, readonly SquadMember[]> = {};
    floorSquadIds.forEach((id, index) => {
      const list = membersBySquad[index];
      if (list) members[id] = list;
    });
    return layoutFloor({ id: floor?.id ?? "" }, squads, agents, members);
  }, [floor, floorSquadIds, membersBySquad, squads, agents]);

  const workingAgents = useMemo(
    () => new Set(tasks.filter((task) => task.status === "running").map((task) => task.agent_id)),
    [tasks],
  );

  // Issues only feed the tag text; they decide nobody's seat.
  const focusOf = useMemo(() => focusIssues(issues, visibleStatuses, squads), [issues, visibleStatuses, squads]);
  const needsYou = EMPTY_SET;

  const sessions = useMemo<WorkerSession[]>(
    () =>
      layout.sessions.map((session) => ({
        key: session.agentId,
        working: workingAgents.has(session.agentId),
        room: session.slot,
      })),
    [layout, workingAgents],
  );

  const tagText = useCallback(
    (key: string) => {
      const tag = officeTagOf(key, needsYou, focusOf);
      if (tag.kind === "needs_you") return t(($) => $.office.needs_you);
      return tag.kind === "status" ? labelOf(tag.status as IssueStatus) : t(($) => $.office.idle);
    },
    [needsYou, focusOf, labelOf, t],
  );

  // One panel manages an agent, whether the click landed on the agent or on its tag.
  const [panel, setPanel] = useState<OfficeAgentTarget | null>(null);
  const [boardOpen, setBoardOpen] = useState(false);

  const onPick = useCallback(
    (agentId: string) => setPanel({ agentId, issueId: focusOf.get(agentId)?.id }),
    [focusOf],
  );
  const onOpenAgent = useCallback((agentId: string) => setPanel({ agentId }), []);

  const roomNames = useMemo(
    () => layout.rooms.map((room) => room.squad?.name ?? t(($) => $.office.room_empty)),
    [layout, t],
  );
  const openCreateSquad = useCallback(() => useModalStore.getState().open("create-squad"), []);

  return (
    <>
      <OfficeCanvas sessions={sessions} tagText={tagText} onPick={onPick} onMove={onOpenAgent} roomNames={roomNames}>
        {(stage) => (
          <>
            <OfficeBoardButton stage={stage} onOpen={() => setBoardOpen(true)} />
            <OfficeRoomOverlays stage={stage} rooms={layout.rooms} onCreateSquad={openCreateSquad} onOpenSquad={() => {}} />
          </>
        )}
      </OfficeCanvas>
      <OfficeBoardDialog open={boardOpen} initialProjectId={projectId} onClose={() => setBoardOpen(false)} />
      <OfficeAgentPanel target={panel} issues={issues} projectId={projectId} onClose={() => setPanel(null)} />
    </>
  );
}

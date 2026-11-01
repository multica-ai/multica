"use client";

import { memo, useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  OFFICE_ROOMS,
  agentTaskSnapshotOptions,
  officeRoomOf,
  useUpdateAgentOfficeRoom,
  type OfficeRoom,
} from "@multica/core/agents";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, AgentTask, Issue } from "@multica/core/types";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { BoardView, type BoardViewProps } from "../../issues/components/board-view";
import { useStatusLabel } from "../../issues/utils/status-label";
import { OfficeAgentPanel, type OfficeAgentTarget } from "./office-agent-panel";
import { OfficeCanvas, type WorkerSession } from "./office-canvas";
import { OfficeBoardButton } from "./office-board-button";
import { OfficeBoardDialog } from "./office-board-dialog";

const EMPTY_TASKS: AgentTask[] = [];
const EMPTY_AGENTS: Agent[] = [];

/**
 * The board, drawn as the pixtuoid office. Each agent with an issue is a worker at
 * a desk, and the tag over the worker names its issue's status. A click on the
 * worker or on its tag opens the one panel that manages the agent: its room, its
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
  const moveAgent = useUpdateAgentOfficeRoom();
  const agentById = useMemo(() => new Map(agents.map((agent) => [agent.id, agent])), [agents]);

  // The project's agents: every agent assigned to one of its issues, each seated once.
  const agentIds = useMemo(() => {
    const ids = new Set<string>();
    for (const issue of issues) {
      if (issue.assignee_type === "agent" && issue.assignee_id) ids.add(issue.assignee_id);
    }
    return [...ids].sort();
  }, [issues]);

  const workingAgents = useMemo(
    () => new Set(tasks.filter((task) => task.status === "running").map((task) => task.agent_id)),
    [tasks],
  );

  // What each agent shows: its issue in progress, else its first open issue. Done issues are not shown.
  const focusOf = useMemo(() => {
    const map = new Map<string, Issue>();
    const shown = new Set(visibleStatuses);
    for (const issue of issues) {
      if (!issue.assignee_id || issue.assignee_type !== "agent") continue;
      if (issue.status === "done" || !shown.has(issue.status)) continue;
      const current = map.get(issue.assignee_id);
      if (!current || (issue.status === "in_progress" && current.status !== "in_progress")) {
        map.set(issue.assignee_id, issue);
      }
    }
    return map;
  }, [issues, visibleStatuses]);

  const roomOf = useCallback(
    (agentId: string): OfficeRoom => {
      const agent = agentById.get(agentId);
      return agent ? officeRoomOf(agent) : officeRoomOf({});
    },
    [agentById],
  );

  const sessions = useMemo<WorkerSession[]>(
    () =>
      agentIds.map((id) => ({
        key: id,
        working: workingAgents.has(id),
        room: OFFICE_ROOMS.indexOf(roomOf(id)),
      })),
    [agentIds, workingAgents, roomOf],
  );

  const occupancy = useMemo(() => {
    const counts: Record<OfficeRoom, number> = { product: 0, research: 0, planning: 0, general: 0 };
    for (const id of agentIds) counts[roomOf(id)] += 1;
    return counts;
  }, [agentIds, roomOf]);

  const tagText = useCallback(
    (key: string) => {
      const issue = focusOf.get(key);
      return issue ? labelOf(issue.status) : t(($) => $.office.idle);
    },
    [focusOf, labelOf, t],
  );

  // One panel manages an agent, whether the click landed on the agent or on its tag.
  const [panel, setPanel] = useState<OfficeAgentTarget | null>(null);
  const [boardOpen, setBoardOpen] = useState(false);

  const onPick = useCallback(
    (agentId: string) => setPanel({ agentId, issueId: focusOf.get(agentId)?.id }),
    [focusOf],
  );
  const onMove = useCallback((agentId: string) => setPanel({ agentId }), []);

  const roomNames = useMemo<Record<OfficeRoom, string>>(
    () => ({
      product: t(($) => $.office.room_product),
      research: t(($) => $.office.room_research),
      planning: t(($) => $.office.room_planning),
      general: t(($) => $.office.room_general),
    }),
    [t],
  );
  const roomNameList = useMemo(() => OFFICE_ROOMS.map((room) => roomNames[room]), [roomNames]);

  const moveTo = useCallback(
    (agentId: string, room: OfficeRoom) => moveAgent.mutateAsync({ agentId, room }).then(() => undefined),
    [moveAgent],
  );

  if (visibleStatuses.length === 0) return <OfficeEmpty />;
  return (
    <>
      <OfficeCanvas sessions={sessions} tagText={tagText} onPick={onPick} onMove={onMove} roomNames={roomNameList}>
        {(stage) => <OfficeBoardButton stage={stage} onOpen={() => setBoardOpen(true)} />}
      </OfficeCanvas>
      <OfficeBoardDialog open={boardOpen} initialProjectId={projectId} onClose={() => setBoardOpen(false)} />
      <OfficeAgentPanel
        target={panel}
        issues={issues}
        projectId={projectId}
        currentRoom={roomOf}
        occupancy={occupancy}
        roomNames={roomNames}
        onMoveRoom={moveTo}
        onClose={() => setPanel(null)}
      />
    </>
  );
}

/** Shown when no status is visible, so the office has no floor to draw. */
function OfficeEmpty() {
  const { t } = useT("issues");
  return (
    <div className="flex flex-1 items-center justify-center p-6 text-body text-white/80">
      {t(($) => $.board.empty_grouping)}
    </div>
  );
}

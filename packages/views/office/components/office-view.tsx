"use client";

import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { initiativeListOptions, type Initiative } from "@multica/core/pipeline";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  layoutFloor,
  officeFloorsOptions,
  officeSquadMembersOptions,
  proposalDetailOptions,
  proposalListOptions,
  useOfficeFloorStore,
  type Proposal,
} from "@multica/core/office";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useModalStore } from "@multica/core/modals";
import type { Agent, AgentTask, IssueStatus, Squad, SquadMember } from "@multica/core/types";
import type { OfficeFloor } from "@multica/core/office";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { BoardView, type BoardViewProps } from "../../issues/components/board-view";
import { useStatusLabel } from "../../issues/utils/status-label";
import { focusIssues, leadersNeedingYou, officeTagOf } from "../lib/office-tag";
import { OfficeAgentPanel, type OfficeAgentTarget } from "./office-agent-panel";
import { OfficeCanvas, type WorkerSession } from "./office-canvas";
import { OfficeBoardButton } from "./office-board-button";
import { OfficeBoardDialog } from "./office-board-dialog";
import { OfficeBriefButton } from "./office-brief-button";
import { OfficeSetupCard } from "./office-setup-card";
import { OfficeRoomOverlays } from "./office-room-overlays";
import { OfficeSeatBanner, OfficeUnseatedTray } from "./office-unseated-tray";
import { OfficeSquadPanel } from "./office-squad-panel";
import { OfficeDecisionsDialog } from "./office-decisions-dialog";
import { OfficeFirstRun } from "./office-first-run";
import { OfficeFloorTabs } from "./office-floor-tabs";
import { ownedOpenCount } from "../../pipeline/pipeline-filters";
import { useOfficeAdmin } from "../lib/use-office-admin";
import { useDecisionQueue } from "../lib/decision-queue";
import { useSeatAll } from "../lib/use-seat-all";

const EMPTY_TASKS: AgentTask[] = [];
const EMPTY_AGENTS: Agent[] = [];
const EMPTY_SQUADS: Squad[] = [];
const EMPTY_FLOORS: OfficeFloor[] = [];
const EMPTY_PROPOSALS: Proposal[] = [];
const EMPTY_INITIATIVES: Initiative[] = [];

/** True while no live squad holds a seat on any floor. */
function noSquadSeated(squads: readonly Squad[], floors: readonly OfficeFloor[]): boolean {
  const floorIds = new Set(floors.map((floor) => floor.id));
  return !squads.some((squad) => !squad.archived_at && squad.floor_id && floorIds.has(squad.floor_id));
}

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
  const squadsQuery = useQuery(squadListOptions(wsId));
  const floorsQuery = useQuery(officeFloorsOptions(wsId));
  const squads = squadsQuery.data ?? EMPTY_SQUADS;
  const floors = floorsQuery.data ?? EMPTY_FLOORS;
  const lastFloorId = useOfficeFloorStore((s) => s.lastFloorId);
  const selectFloor = useOfficeFloorStore((s) => s.setLastFloorId);
  const canManage = useOfficeAdmin();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: pendingAll = EMPTY_PROPOSALS } = useQuery(proposalListOptions(wsId, { state: "pending" }));
  const { data: revisingAll = EMPTY_PROPOSALS } = useQuery(proposalListOptions(wsId, { state: "revising" }));
  const { data: initiativesAll = EMPTY_INITIATIVES } = useQuery(initiativeListOptions(wsId));
  const paths = useWorkspacePaths();
  // The queue leaves out proposals put aside with Decide later until their date.
  const { needsDecision: pending } = useDecisionQueue(pendingAll);
  // The dialog lists the whole decision book: the queue, the put aside and the revising.
  const decisionBook = useMemo(() => [...pendingAll, ...revisingAll], [pendingAll, revisingAll]);

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
  // Leaders of squads with a proposal waiting for the boss, for the Needs you tag.
  const needsYou = useMemo(
    () => leadersNeedingYou(squads, new Set(pending.map((proposal) => proposal.squad_id))),
    [squads, pending],
  );
  const pendingBySlot = useMemo(
    () => layout.rooms.map((room) => (room.squad ? pending.filter((proposal) => proposal.squad_id === room.squad?.id).length : 0)),
    [layout, pending],
  );
  const initiativesBySlot = useMemo(
    () => layout.rooms.map((room) => (room.squad ? ownedOpenCount(initiativesAll, room.squad.id) : 0)),
    [layout, initiativesAll],
  );
  const [decisionsOpen, setDecisionsOpen] = useState(false);

  // Telegram links land here as ?decision=<proposal> and ?brief=<brief>. They are read
  // once: the parameters leave the URL, so a reload does not open the dialog again.
  const navigation = useNavigation();
  const { pathname, hash, replace, searchParams } = navigation;
  const decisionParam = searchParams.get("decision");
  const briefParam = searchParams.get("brief");
  const [decisionFocus, setDecisionFocus] = useState<string | null>(null);
  const [briefFocus, setBriefFocus] = useState<string | null>(null);
  useEffect(() => {
    if (!decisionParam && !briefParam) return;
    if (decisionParam) {
      setDecisionFocus(decisionParam);
      setDecisionsOpen(true);
    }
    if (briefParam) setBriefFocus(briefParam);
    const rest = new URLSearchParams(searchParams);
    rest.delete("decision");
    rest.delete("brief");
    const search = rest.toString();
    replace(`${pathname}${search ? `?${search}` : ""}${hash}`);
  }, [decisionParam, briefParam, searchParams, pathname, hash, replace]);

  // A linked proposal that is no longer pending or revising is fetched, so the dialog can show its state.
  const linkedInBook = decisionFocus !== null && decisionBook.some((proposal) => proposal.id === decisionFocus);
  const { data: linkedProposal } = useQuery({
    ...proposalDetailOptions(wsId, decisionFocus ?? ""),
    enabled: !!wsId && !!decisionFocus && !linkedInBook,
  });
  const decisionProposals = useMemo(
    () => (linkedProposal && !linkedInBook ? [...decisionBook, linkedProposal] : decisionBook),
    [decisionBook, linkedProposal, linkedInBook],
  );

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
  const [squadPanel, setSquadPanel] = useState<string | null>(null);

  const onPick = useCallback(
    (agentId: string) => setPanel({ agentId, issueId: focusOf.get(agentId)?.id }),
    [focusOf],
  );
  const onOpenAgent = useCallback((agentId: string) => setPanel({ agentId }), []);

  const roomNames = useMemo(
    () => layout.rooms.map((room) => room.squad?.name ?? t(($) => $.office.room_empty)),
    [layout, t],
  );
  const openSquad = useCallback((slot: number) => setSquadPanel(layout.rooms[slot]?.squad?.id ?? null), [layout]);
  const roomPickable = useCallback((slot: number) => !!layout.rooms[slot]?.squad, [layout]);
  const openCreateSquad = useCallback(() => useModalStore.getState().open("create-squad"), []);

  // Floors that still hold a live squad, and whether any squad is seated at all.
  const occupied = useMemo(
    () => new Set(squads.filter((squad) => !squad.archived_at && squad.floor_id).map((squad) => squad.floor_id as string)),
    [squads],
  );
  const { seatAll, pending: seating } = useSeatAll(floors, squads);
  const canSeat = useCallback((squad: Squad) => canManage || squad.creator_id === userId, [canManage, userId]);
  const loaded = squadsQuery.isSuccess && floorsQuery.isSuccess;
  const firstRun = loaded && noSquadSeated(squads, floors);

  return (
    <>
      <OfficeFloorTabs
        floors={floors}
        currentId={floor?.id ?? null}
        occupied={occupied}
        canManage={canManage}
        onSelect={selectFloor}
      >
        <OfficeBriefButton focusBriefId={briefFocus} />
        {pendingAll.length > 0 ? (
          <Button type="button" variant="outline" size="sm" onClick={() => setDecisionsOpen(true)}>
            {t(($) => $.office.decisions_open)}
            {pending.length > 0 ? <span className="tabular-nums text-muted-foreground">{pending.length}</span> : null}
          </Button>
        ) : null}
      </OfficeFloorTabs>
      {canManage && layout.unseatedSquads.length > 0 ? (
        <OfficeSeatBanner count={layout.unseatedSquads.length} pending={seating} onSeatAll={seatAll} />
      ) : null}
      <OfficeSetupCard />
      {firstRun ? (
        <OfficeFirstRun />
      ) : (
        // A floor is its own office: a new worker per floor seats its workers at their desks from the
        // first frame, instead of walking the previous floor's workers out of the door.
        <OfficeCanvas
          key={floor?.id ?? "no-floor"}
          sessions={sessions}
          tagText={tagText}
          onPick={onPick}
          onMove={onOpenAgent}
          roomNames={roomNames}
          roomPickable={roomPickable}
          onRoomPick={openSquad}
        >
          {(stage) => (
            <>
              <OfficeBoardButton stage={stage} onOpen={() => setBoardOpen(true)} />
              <OfficeRoomOverlays
                stage={stage}
                rooms={layout.rooms}
                pendingBySlot={pendingBySlot}
                initiativesBySlot={initiativesBySlot}
                onCreateSquad={openCreateSquad}
                onOpenSquad={openSquad}
                onOpenDecisions={() => setDecisionsOpen(true)}
                onOpenPipeline={(squadId) => navigation.push(paths.pipelineForSquad(squadId))}
              />
            </>
          )}
        </OfficeCanvas>
      )}
      <OfficeUnseatedTray
        squads={layout.unseatedSquads}
        agents={layout.unassignedAgents}
        floors={floors}
        allSquads={squads}
        canSeat={canSeat}
      />
      <OfficeBoardDialog open={boardOpen} initialProjectId={projectId} onClose={() => setBoardOpen(false)} />
      <OfficeSquadPanel
        squadId={squadPanel}
        room={layout.rooms.find((room) => room.squad?.id === squadPanel) ?? null}
        onPickAgent={(agentId) => {
          setSquadPanel(null);
          onOpenAgent(agentId);
        }}
        onReview={() => {
          setSquadPanel(null);
          setDecisionsOpen(true);
        }}
        onClose={() => setSquadPanel(null)}
      />
      <OfficeDecisionsDialog
        open={decisionsOpen}
        proposals={decisionProposals}
        squads={squads}
        canDecide={canManage}
        focusId={decisionFocus}
        onClose={() => {
          setDecisionsOpen(false);
          setDecisionFocus(null);
        }}
      />
      <OfficeAgentPanel target={panel} issues={issues} projectId={projectId} onClose={() => setPanel(null)} />
    </>
  );
}

"use client";

import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useAuthStore } from "@multica/core/auth";
import {
  companyFloorsOptions,
  companySquadMembersOptions,
  proposalListOptions,
  squadOpenItemsOptions,
  squadRoutinesOptions,
  type FloorRoom,
} from "@multica/core/company";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentListOptions, squadListOptions, squadMemberStatusOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { autonomyLevelName, squadAutonomyOptions } from "@multica/core/governance";
import type { SquadMemberStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { AUTONOMY_LEVEL_KEYS } from "../../governance/autonomy-levels";
import { useCompanyAdmin } from "../lib/use-company-admin";
import { CompanyCharterTab } from "./company-charter-tab";
import { CompanyRoutines } from "./company-routines";
import { CompanySquadAttention } from "./company-squad-attention";
import { CompanySquadSeatMenu } from "./company-squad-seat-menu";
import { projectListOptions } from "@multica/core/projects/queries";
import { useAddSquadToCompany } from "../lib/use-add-squad-to-company";
import { useIssuePeekActions } from "../../issues/surface/peek-context";

const STATUS_KEYS = {
  working: "status_working",
  idle: "status_idle",
  offline: "status_offline",
  unstable: "status_unstable",
  archived: "status_archived",
} as const;

/**
 * One squad's room, from the room name or its `+N` chip: its members and their
 * state, its routines and autopilots, the proposals waiting on the boss, and its
 * charter. It sits beside the agent panel, which a click on a member opens.
 */
export function CompanySquadPanel({
  squadId,
  room,
  onPickAgent,
  onReview,
  onAssignProject,
  onClose,
}: {
  /** The squad to show; the panel is closed while this is null. */
  squadId: string | null;
  /** The squad's room on the floor, for who has a desk and who overflows. */
  room: FloorRoom | null;
  onPickAgent: (agentId: string) => void;
  /** Opens the decisions dialog. */
  onReview?: () => void;
  /** Opens the dialog that gives the squad a project. */
  onAssignProject?: (squadId: string) => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={squadId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,40rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg">
        {squadId ? (
          <PanelBody
            key={squadId}
            squadId={squadId}
            room={room}
            onPickAgent={onPickAgent}
            onReview={onReview}
            onAssignProject={onAssignProject}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function PanelBody({
  squadId,
  room,
  onPickAgent,
  onReview,
  onAssignProject,
}: {
  squadId: string;
  room: FloorRoom | null;
  onPickAgent: (agentId: string) => void;
  onReview?: () => void;
  onAssignProject?: (squadId: string) => void;
}) {
  const { t } = useT("issues");
  const { t: tSquads } = useT("squads");
  const { t: tAutopilots } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(companySquadMembersOptions(wsId, squadId));
  const { data: status } = useQuery(squadMemberStatusOptions(wsId, squadId));
  const { data: autopilots = [] } = useQuery(autopilotListOptions(wsId));
  const { data: proposals = [] } = useQuery(proposalListOptions(wsId, { state: "pending", squad_id: squadId }));
  const { data: routines = [] } = useQuery(squadRoutinesOptions(wsId, squadId));
  const { data: openItems } = useQuery(squadOpenItemsOptions(wsId, squadId));
  const peek = useIssuePeekActions();
  const { data: autonomy } = useQuery(squadAutonomyOptions(wsId, squadId));
  const { data: floors = [] } = useQuery(companyFloorsOptions(wsId));
  const { addSquad, pending: adding } = useAddSquadToCompany(floors, squads);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const project = projects.find((p) => p.squad_id === squadId && p.status !== "completed" && p.status !== "cancelled");
  const { t: tGovernance } = useT("governance");
  const isCompanyAdmin = useCompanyAdmin();
  const userId = useAuthStore((s) => s.user?.id);

  const squad = squads.find((candidate) => candidate.id === squadId);
  // The same gate as the server's canManageSquad: a workspace owner or admin, or the squad's creator.
  const canManage = isCompanyAdmin || (!!squad && !!userId && squad.creator_id === userId);
  const statusOf = new Map<string, SquadMemberStatus>((status?.members ?? []).map((entry) => [entry.member_id, entry]));
  const seated = new Set(room?.agentIds ?? []);
  const homeOf = new Map(agents.map((agent) => [agent.id, agent.home_squad_id]));
  // Routines are listed in their own section, so they are left out of the autopilots.
  const routineIds = new Set(routines.map((routine) => routine.autopilot_id));
  const squadAutopilots = autopilots.filter(
    (autopilot) =>
      autopilot.assignee_type === "squad" &&
      autopilot.assignee_id === squadId &&
      autopilot.status !== "archived" &&
      !routineIds.has(autopilot.id),
  );

  return (
    <Tabs defaultValue="overview" className="flex min-h-0 flex-1 flex-col gap-0">
      <header className="flex shrink-0 flex-col gap-1 border-b px-4 py-3 pr-12">
        <DialogTitle className="truncate text-heading">{squad?.name ?? ""}</DialogTitle>
        {squad?.description ? <p className="text-body text-muted-foreground">{squad.description}</p> : null}
        {project ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.company.panel_project, { title: project.title })}</p>
        ) : null}
        {isCompanyAdmin && squad && !squad.floor_id ? (
          <div className="pt-1">
            <Button type="button" size="sm" disabled={adding} onClick={() => void addSquad(squad)}>
              {t(($) => $.company.squad_add_to_company)}
            </Button>
          </div>
        ) : null}
        {canManage && squad && onAssignProject ? (
          <div className="pt-1">
            <Button type="button" variant="outline" size="sm" onClick={() => onAssignProject(squadId)}>
              {t(($) => $.company.assign_open)}
            </Button>
          </div>
        ) : null}
        {isCompanyAdmin && squad && squad.floor_id && floors.length > 0 ? (
          <div className="pt-1">
            <CompanySquadSeatMenu
              squad={squad}
              floors={floors}
              allSquads={squads}
              trigger={<Button type="button" variant="outline" size="sm" />}
            >
              {t(($) => $.company.squad_move)}
            </CompanySquadSeatMenu>
          </div>
        ) : null}
      </header>
      <div className="shrink-0 border-b px-4 py-2">
        <TabsList>
          <TabsTrigger value="overview">{t(($) => $.company.panel_tab_overview)}</TabsTrigger>
          <TabsTrigger value="charter">{t(($) => $.company.panel_tab_charter)}</TabsTrigger>
        </TabsList>
      </div>
      <TabsContent value="overview" className="flex min-h-0 flex-col gap-4 overflow-y-auto px-4 py-3">
        <section aria-label={t(($) => $.company.squad_members)} className="flex flex-col gap-1">
          <h3 className="flex items-center gap-2 text-caption font-medium text-muted-foreground">
            {t(($) => $.company.squad_members)}
            {room && room.overflow > 0 ? (
              <span className="rounded-full bg-muted px-1.5 py-0.5 tabular-nums">{`+${room.overflow}`}</span>
            ) : null}
          </h3>
          {members.map((member) => {
            const name = getActorName(member.member_type, member.member_id);
            const entry = statusOf.get(member.member_id);
            const state = entry?.status && entry.status in STATUS_KEYS ? entry.status : null;
            const noDesk =
              member.member_type === "agent" && homeOf.get(member.member_id) === squadId && !seated.has(member.member_id);
            const row = (
              <>
                <ActorAvatar actorType={member.member_type} actorId={member.member_id} size="sm" />
                <span className="min-w-0 flex-1 truncate text-left">{name}</span>
                {noDesk ? <span className="text-caption text-muted-foreground">{t(($) => $.company.no_desk)}</span> : null}
                {state ? (
                  <span className="text-caption text-muted-foreground">
                    {tSquads(($) => $.members_tab[STATUS_KEYS[state as keyof typeof STATUS_KEYS]])}
                  </span>
                ) : null}
              </>
            );
            return member.member_type === "agent" ? (
              <button
                key={member.id}
                type="button"
                onClick={() => onPickAgent(member.member_id)}
                className="flex min-h-9 items-center gap-2 rounded-md px-2 text-body hover:bg-accent"
              >
                {row}
              </button>
            ) : (
              <div key={member.id} className="flex min-h-9 items-center gap-2 px-2 text-body">
                {row}
              </div>
            );
          })}
        </section>
        <CompanySquadAttention squadId={squadId} canPin={isCompanyAdmin} />
        <section aria-label={tGovernance(($) => $.autonomy.panel_heading)} className="flex flex-col gap-1">
          <h3 className="text-caption font-medium text-muted-foreground">{tGovernance(($) => $.autonomy.panel_heading)}</h3>
          <p className="text-body">
            {tGovernance(($) => $.autonomy.level_tag, { level: autonomy?.level ?? squad?.autonomy_level ?? 1 })}
            {" "}
            {(() => {
              const level = autonomy?.level ?? squad?.autonomy_level ?? 1;
              const key = AUTONOMY_LEVEL_KEYS[level];
              return key ? tGovernance(($) => $.autonomy.levels[key].name) : autonomyLevelName(level);
            })()}
          </p>
          {autonomy?.reason ? <p className="text-caption text-muted-foreground">{autonomy.reason}</p> : null}
        </section>
        {openItems && openItems.issues.length > 0 ? (
          <section aria-label={t(($) => $.company.squad_open_items)} className="flex flex-col gap-1">
            <h3 className="flex items-center gap-2 text-caption font-medium text-muted-foreground">
              {t(($) => $.company.squad_open_items)}
              {openItems.total > openItems.issues.length ? (
                <span className="rounded-full bg-muted px-1.5 py-0.5 tabular-nums">{`+${openItems.total - openItems.issues.length}`}</span>
              ) : null}
            </h3>
            {openItems.issues.map((issue) => (
              <button
                key={issue.id}
                type="button"
                onClick={() => peek?.open(issue.id)}
                disabled={!peek}
                className="flex min-h-9 items-center gap-2 rounded-md px-2 text-left text-body hover:bg-accent disabled:hover:bg-transparent"
              >
                <span className="shrink-0 text-caption text-muted-foreground">{issue.identifier}</span>
                <span className="min-w-0 flex-1 truncate">{issue.title}</span>
              </button>
            ))}
          </section>
        ) : null}
        <CompanyRoutines squadId={squadId} canManage={canManage} />
        {squadAutopilots.length > 0 ? (
          <section aria-label={t(($) => $.company.squad_autopilots)} className="flex flex-col gap-1">
            <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.squad_autopilots)}</h3>
            {squadAutopilots.map((autopilot) => (
              <div key={autopilot.id} className="flex min-h-9 items-center gap-2 px-2 text-body">
                <span className="min-w-0 flex-1 truncate">{autopilot.title}</span>
                <span className="text-caption text-muted-foreground">
                  {autopilot.status === "active"
                    ? tAutopilots(($) => $.status.active)
                    : tAutopilots(($) => $.status.paused)}
                </span>
              </div>
            ))}
          </section>
        ) : null}
        {proposals.length > 0 ? (
          <section aria-label={t(($) => $.company.squad_proposals)} className="flex flex-col gap-1">
            <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.company.squad_proposals)}</h3>
            {proposals.map((proposal) => (
              <button
                key={proposal.id}
                type="button"
                onClick={onReview}
                disabled={!onReview}
                className="flex min-h-9 items-center gap-2 rounded-md px-2 text-left text-body hover:bg-accent disabled:hover:bg-transparent"
              >
                <span className="shrink-0 text-caption text-muted-foreground">{proposal.issue_identifier}</span>
                <span className="min-w-0 flex-1 truncate">{proposal.title}</span>
              </button>
            ))}
          </section>
        ) : null}
      </TabsContent>
      <TabsContent value="charter" className="min-h-0 overflow-y-auto px-4 py-3">
        <CompanyCharterTab squadId={squadId} canEdit={canManage} />
      </TabsContent>
    </Tabs>
  );
}

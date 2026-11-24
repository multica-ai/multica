"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CircleEqual, Flag, Plus, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { useUpdateIssueBlockers, useUpdateIssueBlocking } from "@multica/core/issues/blockers";
import { issueDuplicatesOptions } from "@multica/core/issues/queries";
import { workflowAllowsStatus } from "@multica/core/issue-workflows";
import { useModalStore } from "@multica/core/modals";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type { Issue } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { IssueSidebarCollapsible, IssueSidebarSectionHeader } from "./issue-sidebar-section";
import { isIssueWaiting } from "../utils/waiting";
import { issueRelations } from "../utils/relations";
import { useIssueStatusOptions } from "../utils/status-options";
import { StatusIcon } from "./status-icon";

const MARKER_MAX_ROWS = 8;
const stopPress = (e: React.SyntheticEvent) => e.stopPropagation();

/**
 * The red flag on list rows, board cards and table titles. A bare count says
 * nothing about what it counts, so hovering opens a card naming the blockers
 * and what happens once they end — the sidebar's Waiting section, in brief.
 */
export function IssueBlockedMarker({ issue, delay }: { issue: Issue; /** Tests only. */ delay?: number }) {
  const { t } = useT("issues");
  if (!isIssueWaiting(issue)) return null;
  const count = issue.blocked_count ?? 0;
  const label = t(($) => $.waiting.count, { count });
  return <HoverCard>
    {/* A span, not the trigger's default anchor: rows and cards are links. */}
    <HoverCardTrigger delay={delay} render={<span />} aria-label={label}
      className="inline-flex shrink-0 cursor-default items-center gap-1 text-destructive text-caption">
      <Flag className="size-3.5" aria-hidden="true" />{count}
    </HoverCardTrigger>
    <HoverCardContent align="start" className="w-80 max-w-[calc(100vw-1rem)] p-0">
      <IssueBlockedMarkerCard issue={issue} />
    </HoverCardContent>
  </HoverCard>;
}

// Mounted only while the card is open, so the catalog and directory reads
// cost nothing per row.
function IssueBlockedMarkerCard({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  const { getActorName } = useActorName();
  const count = issue.blocked_count ?? 0;
  const blockers = issue.blocked_by ?? [];
  // Unfinished first; sort is stable, so each group keeps the server order.
  const sorted = [...blockers].sort((a, b) => Number(a.closed) - Number(b.closed));
  const shown = sorted.slice(0, MARKER_MAX_ROWS);
  const restricted = Math.max(0, count - blockers.filter((b) => !b.closed).length);
  const finished = blockers.length - blockers.filter((b) => !b.closed).length;
  const total = blockers.length + restricted;
  const runsAgent = issue.assignee_type === "agent" || issue.assignee_type === "squad";
  const { data: agents } = useQuery({ ...agentListOptions(wsId), enabled: runsAgent });
  const { data: squads } = useQuery({ ...squadListOptions(wsId), enabled: issue.assignee_type === "squad" });
  const assigneeName = issue.assignee_type && issue.assignee_id
    ? getActorName(issue.assignee_type, issue.assignee_id) : null;
  // The agent that would run: the assignee itself, or a squad's leader.
  // Undefined while the directories load, so the card never flashes a warning.
  const runnerId = issue.assignee_type === "squad"
    ? (squads ? squads.find((s) => s.id === issue.assignee_id)?.leader_id ?? null : undefined)
    : issue.assignee_id;
  const runner = runnerId === undefined || !agents ? undefined : agents.find((a) => a.id === runnerId) ?? null;
  const cannotRun = runsAgent && runner !== undefined && (!runner || !runner.runtime_id || !!runner.archived_at);
  // Mirrors the server: Backlog stays parked, a member gets an inbox
  // notification, an agent or squad starts only if that agent can run, and
  // nobody is woken without an assignee.
  const footer = issue.status === "backlog" ? t(($) => $.waiting.hint_backlog)
    : !assigneeName ? t(($) => $.waiting.marker_unassigned)
    : issue.assignee_type === "member" ? t(($) => $.waiting.marker_notify, { name: assigneeName })
    : cannotRun ? t(($) => $.waiting.marker_unavailable, { name: assigneeName })
    : t(($) => $.waiting.marker_auto_start, { name: assigneeName });
  return <div className="text-caption">
    <p className="px-3 pt-2.5 pb-1.5 font-medium text-foreground">
      {t(($) => $.waiting.marker_title, { count })}
      {finished > 0 && <span className="font-normal text-muted-foreground">
        {" · "}{t(($) => $.waiting.marker_ended, { finished, total })}
      </span>}
    </p>
    <div className="px-1.5">
      {shown.map((blocker) => <AppLink key={blocker.id} href={paths.issueDetail(issueRouteRef(blocker))} title={blocker.title}
        onMouseDown={stopPress} onPointerDown={stopPress}
        className="flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 hover:bg-accent">
        <StatusIcon status={blocker.status} category={categoryOf(blocker.status)} color={colorOf(blocker.status)} icon={iconOf(blocker.status)} className="size-3.5 shrink-0" />
        <span className="shrink-0 text-muted-foreground">{blocker.identifier}</span>
        <span className={blocker.closed ? "min-w-0 truncate text-muted-foreground" : "min-w-0 truncate text-foreground"}>{blocker.title}</span>
      </AppLink>)}
      {sorted.length > shown.length && <p className="px-1.5 py-1 text-muted-foreground">
        {t(($) => $.waiting.marker_more, { count: sorted.length - shown.length })}
      </p>}
      {restricted > 0 && <p className="px-1.5 py-1 text-muted-foreground">{t(($) => $.waiting.restricted, { count: restricted })}</p>}
    </div>
    <p className="mt-1.5 border-t border-border px-3 py-2 text-muted-foreground">{footer}</p>
  </div>;
}

/**
 * The relation types a reader can add, for the Relations section's "+" and
 * the sidebar add row. Each opens the same modal as the Relations submenu of
 * the issue actions menu.
 */
export function IssueRelationTypeItems({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const openModal = useModalStore((s) => s.open);
  const { workflow } = useIssueStatusOptions(wsId, issue.project_id, issue.status);
  const item = (icon: React.ReactNode, label: string, hint: string, onClick: () => void) => (
    <DropdownMenuItem onClick={onClick} className="items-start">
      {icon}
      <span className="flex min-w-0 flex-col">
        <span>{label}</span>
        <span className="text-caption text-muted-foreground">{hint}</span>
      </span>
    </DropdownMenuItem>
  );
  return <>
    {item(<Flag className="mt-0.5 text-destructive" />, t(($) => $.actions.add_blocker), t(($) => $.relations.add_blocker_hint),
      () => openModal("issue-add-blocker", { issueId: issue.id }))}
    {item(<Flag className="mt-0.5 text-warning" />, t(($) => $.actions.add_blocking), t(($) => $.relations.add_blocking_hint),
      () => openModal("issue-add-blocking", { issueId: issue.id }))}
    {/* Marking a duplicate cancels the issue. (MUL-7420) */}
    {workflowAllowsStatus(workflow, "cancelled") && <>
      <DropdownMenuSeparator />
      {item(<CircleEqual className="mt-0.5" />, t(($) => $.actions.mark_duplicate), t(($) => $.relations.mark_duplicate_hint),
        () => openModal("issue-mark-duplicate", { issueId: issue.id }))}
    </>}
  </>;
}

/**
 * Blockers, the issues this one blocks, and its duplicates in one section
 * (MUL-10050). Only unfinished blockers are listed; the section disappears
 * when there is nothing to list.
 */
export function IssueRelationsSection({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const blockedByMutation = useUpdateIssueBlockers(wsId, issue.id);
  const blockingMutation = useUpdateIssueBlocking(wsId, issue.id);
  const { data } = useQuery(issueDuplicatesOptions(wsId, issue.id));
  const { categoryOf, colorOf, iconOf } = useIssueStatuses(wsId);
  if (issue.blocked_by === undefined) return null;
  const relations = issueRelations(issue, data?.duplicates);
  if (relations.empty) return null;
  const { blockedBy, blocking, duplicates, restricted, finished, total } = relations;
  const pending = blockedByMutation.isPending || blockingMutation.isPending;
  const error = blockedByMutation.error ?? blockingMutation.error;
  const statusIcon = (status: string) =>
    <StatusIcon status={status} category={categoryOf(status)} color={colorOf(status)} icon={iconOf(status)} className="size-3.5 shrink-0" />;
  const stageTag = <span title={t(($) => $.relations.stage_tag_hint)} className="shrink-0 rounded-xs bg-muted px-1 text-micro text-muted-foreground">
    {t(($) => $.relations.stage_tag)}
  </span>;
  return <div>
    <IssueSidebarSectionHeader title={t(($) => $.relations.title)} open={open} onOpenChange={setOpen}
      count={blockedBy.length + restricted + blocking.length + duplicates.length}
      actions={<DropdownMenu>
        <DropdownMenuTrigger render={<Button variant="ghost" size="icon-xs" aria-label={t(($) => $.relations.add)} />}><Plus /></DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64"><IssueRelationTypeItems issue={issue} /></DropdownMenuContent>
      </DropdownMenu>} />
    <IssueSidebarCollapsible open={open}><div className="px-2">
      {(blockedBy.length > 0 || restricted > 0) && <>
        <RelationGroup icon={<Flag className="size-3.5 text-destructive" />} label={t(($) => $.relations.blocked_by)}
          aside={finished > 0 ? t(($) => $.waiting.marker_ended, { finished, total }) : undefined} />
        {blockedBy.map((blocker) => <RelationRow key={blocker.id} issue={blocker} icon={statusIcon(blocker.status)}
          tag={blocker.via === "stage" ? stageTag : undefined} removeLabel={t(($) => $.waiting.remove, { identifier: blocker.identifier })} pending={pending}
          onRemove={blocker.via === "manual" ? () => blockedByMutation.mutate({ remove_blocked_by: [blocker.id] }) : undefined} />)}
        {restricted > 0 && <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.waiting.restricted, { count: restricted })}</p>}
        {/* Only the fixed backlog key parks work, matching the server's queue admission. */}
        {isIssueWaiting(issue) && <p className="px-2 pt-0.5 pb-1 text-caption text-muted-foreground">
          {issue.status === "backlog" ? t(($) => $.waiting.hint_backlog) : t(($) => $.waiting.hint)}
        </p>}
      </>}
      {blocking.length > 0 && <>
        <RelationGroup icon={<Flag className="size-3.5 text-warning" />} label={t(($) => $.relations.blocking)} />
        {blocking.map((dependent) => <RelationRow key={dependent.id} issue={dependent} icon={statusIcon(dependent.status)}
          tag={dependent.via === "stage" ? stageTag : undefined} removeLabel={t(($) => $.waiting.remove, { identifier: dependent.identifier })} pending={pending}
          onRemove={dependent.via === "manual" ? () => blockingMutation.mutate({ dependentId: dependent.id, remove: true }) : undefined} />)}
      </>}
      {duplicates.length > 0 && <>
        <RelationGroup icon={<CircleEqual className="size-3.5" />} label={t(($) => $.duplicates.section_title)} />
        {duplicates.map((duplicate) => <RelationRow key={duplicate.id} issue={duplicate} icon={statusIcon(duplicate.status)} />)}
      </>}
      {error && <p role="alert" className="px-2 py-1 text-caption text-destructive">{error.message}</p>}
    </div></IssueSidebarCollapsible>
  </div>;
}

function RelationGroup({ icon, label, aside }: { icon: React.ReactNode; label: string; aside?: string }) {
  return <div className="flex min-w-0 items-center gap-1.5 px-2 pt-2 pb-0.5 text-caption font-medium text-muted-foreground first:pt-0">
    {icon}<span className="truncate">{label}</span>
    {aside && <span className="ml-auto shrink-0 font-normal tabular-nums">{aside}</span>}
  </div>;
}

function RelationRow({ issue, icon, tag, onRemove, removeLabel, pending }: {
  issue: Pick<Issue, "id" | "identifier" | "title">;
  icon: React.ReactNode;
  tag?: React.ReactNode;
  onRemove?: () => void;
  removeLabel?: string;
  pending?: boolean;
}) {
  const paths = useWorkspacePaths();
  return <div className="group/relation flex min-w-0 items-center gap-1 rounded-md px-2 transition-colors hover:bg-accent/50">
    <AppLink href={paths.issueDetail(issueRouteRef(issue))} title={issue.title} className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 text-label">
      {icon}
      <span className="shrink-0 text-muted-foreground">{issue.identifier}</span>
      <span className="truncate group-hover/relation:text-foreground">{issue.title}</span>
    </AppLink>
    {tag}
    {onRemove && <Button variant="ghost" size="icon-xs" disabled={pending} aria-label={removeLabel} onClick={onRemove}
      className="text-muted-foreground opacity-0 group-hover/relation:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"><X /></Button>}
  </div>;
}

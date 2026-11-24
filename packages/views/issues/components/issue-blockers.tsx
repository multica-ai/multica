"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Flag, Plus, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { useUpdateIssueBlockers } from "@multica/core/issues/blockers";
import { useModalStore } from "@multica/core/modals";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type { Issue } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";
import { isIssueWaiting } from "../utils/waiting";
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

export function IssueBlockersSection({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [open, setOpen] = useState(true);
  const openModal = useModalStore((s) => s.open);
  const mutation = useUpdateIssueBlockers(wsId, issue.id);
  if (issue.blocked_by === undefined) return null;
  const blockers = issue.blocked_by;
  const manual = blockers.filter((b) => b.via === "manual");
  const stages = blockers.filter((b) => b.via === "stage");
  const activeStages = stages.filter((b) => !b.closed);
  const stageCount = stages.length - activeStages.length;
  const restrictedCount = Math.max(0, (issue.blocked_count ?? 0) - blockers.filter((b) => !b.closed).length);
  return <div>
    <IssueSidebarSectionHeader title={t(($) => $.waiting.title)} open={open} onOpenChange={setOpen} count={issue.blocked_count}
      actions={<Button variant="ghost" size="icon-xs" aria-label={t(($) => $.waiting.add)} onClick={() => openModal("issue-add-blocker", { issueId: issue.id })}><Plus /></Button>} />
    {open && <div className="px-4 space-y-2 text-caption">
      {manual.map((blocker) => <div key={blocker.id} className="flex min-w-0 items-center gap-1">
        <Flag className={blocker.closed ? "size-3.5 shrink-0 text-muted-foreground" : "size-3.5 shrink-0 text-destructive"} aria-hidden="true" />
        <AppLink href={paths.issueDetail(issueRouteRef(blocker))} className="min-w-0 flex-1 truncate hover:underline" title={blocker.title}>
          <span className="text-muted-foreground">{blocker.identifier}</span>{" "}{blocker.title}
        </AppLink>
        <Button variant="ghost" size="icon-xs" disabled={mutation.isPending} aria-label={t(($) => $.waiting.remove, { identifier: blocker.identifier })}
          onClick={() => mutation.mutate({ remove_blocked_by: [blocker.id] })}><X /></Button>
      </div>)}
      {stages.length > 0 && <div>
        <p className="text-muted-foreground">{t(($) => $.waiting.stage, { finished: stageCount, total: stages.length })}</p>
        {activeStages.map((blocker) => <AppLink key={blocker.id} href={paths.issueDetail(issueRouteRef(blocker))} className="block truncate py-1 hover:underline" title={blocker.title}>
          <span className="text-muted-foreground">{blocker.identifier}</span>{" "}{blocker.title}
        </AppLink>)}
      </div>}
      {restrictedCount > 0 && <p className="text-muted-foreground">{t(($) => $.waiting.restricted, {count: restrictedCount})}</p>}
      {/* Only the fixed backlog key parks work, matching the server's queue admission. */}
      {isIssueWaiting(issue) && <p className="text-muted-foreground">
        {issue.status === "backlog" ? t(($) => $.waiting.hint_backlog) : t(($) => $.waiting.hint)}
      </p>}
      {blockers.length === 0 && !restrictedCount && <p className="text-muted-foreground">{t(($) => $.waiting.empty)}</p>}
      {mutation.isError && <p role="alert" className="text-destructive">{mutation.error.message}</p>}
    </div>}
  </div>;
}

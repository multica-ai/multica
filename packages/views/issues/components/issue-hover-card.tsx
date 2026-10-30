"use client";

import { issueStatusCategory } from "@multica/core/issues";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useStatusLabel } from "./../utils/status-label";
import { useCallback, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Box,
  CalendarDays,
  CornerLeftUp,
  GitPullRequest,
  ListTree,
  Tag,
  UserRound,
  type IconComponent,
} from "@multica/ui/icons";
import { childIssueProgressOptions, issueDetailOptions } from "@multica/core/issues/queries";
import { formatDateOnly, isPastDateOnly } from "@multica/core/issues/date";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { issuePullRequestsOptions } from "@multica/core/github";
import { projectListOptions } from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AgentTask, GitHubPullRequest, Issue } from "@multica/core/types";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { ShimmerText } from "@multica/ui/components/common/shimmer-text";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import { selectIssueTasks } from "../surface/activity";
import { descriptionPreview } from "./description-preview";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { PullRequestInlineSummary } from "./pull-request-list";
import { StatusIcon } from "./status-icon";

const MAX_LABELS = 3;

interface IssueHoverCardProps {
  issueId: string;
  children: ReactNode;
  /**
   * Identifier to name the issue with when the detail fetch fails (e.g.
   * "MUL-7"). The same label the chip degrades to, so a card that cannot load
   * still says which issue it is about.
   */
  fallbackLabel?: string;
  /**
   * Open delay override, in milliseconds. Exists for tests only — production
   * passes nothing, so the card uses Base UI's default of 600ms. Tests pass 0
   * so hover assertions don't need to wait out the real delay.
   */
  delay?: number;
}

/**
 * Detail for an issue mention, on hover.
 *
 * The inline chip shows status, identifier, and as much title as fits inside
 * its `min(18rem, 100%)` cap — so a long title arrives truncated. The card
 * carries what the chip cannot: the full title, the status by name, priority,
 * a description snippet, a property list (assignee and live agent activity,
 * project, parent, labels, due date, sub-issue progress, linked PR), and who
 * created the issue. Issue mentions are the only kind with a hover preview —
 * member and agent mentions render as a plain `.mention` span in
 * rich-content.tsx. This lives here rather than in packages/ui because it
 * reads workspace queries.
 *
 * Assignee, status, and provenance render for every issue, so a freshly filed
 * issue with no description or properties still gets a card that says more
 * than the chip does.
 *
 * Every query lives below IssueHoverCard on purpose: Base UI mounts the popup
 * only while the card is open, so this component adds no request of its own
 * until a card opens. Moving a query up into IssueHoverCard would fire one per
 * mention on render. (The chip inside the trigger has its own
 * unresolved-issue fetch; that is independent of this.)
 */
export function IssueHoverCard({
  issueId,
  children,
  fallbackLabel,
  delay,
}: IssueHoverCardProps) {
  return (
    <HoverCard>
      {/* Rendered as a span, not the trigger's default anchor: the children are
          already an AppLink, and an anchor inside an anchor is invalid. */}
      <HoverCardTrigger delay={delay} render={<span />}>
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-85 max-w-[calc(100vw-1rem)] p-3">
        <IssueHoverCardBody issueId={issueId} fallbackLabel={fallbackLabel} />
      </HoverCardContent>
    </HoverCard>
  );
}

function IssueHoverCardBody({
  issueId,
  fallbackLabel,
}: {
  issueId: string;
  fallbackLabel?: string;
}) {
  const wsId = useWorkspaceId();
  const detail = useQuery(issueDetailOptions(wsId, issueId));
  const { t } = useT("issues");

  // A skeleton rather than localized loading text: only the pending phase gets
  // it, so a settled query never animates forever.
  if (detail.isPending) {
    return (
      <div data-testid="issue-hover-card-skeleton" className="flex flex-col gap-1.5">
        <Skeleton className="h-3 w-24" />
        <Skeleton className="h-4 w-56" />
        <Skeleton className="mt-1.5 h-3 w-44" />
        <Skeleton className="mt-1.5 h-4 w-32" />
      </div>
    );
  }

  const issue = detail.data;

  // Terminal state for every way the detail can fail to arrive: an error after
  // retries, a deleted issue, or a mention the viewer cannot read. Named with
  // the identifier the way the chip degrades, so the card still says which
  // issue it is about.
  if (detail.isError || !issue) {
    return (
      <div className="flex flex-col gap-1.5">
        {fallbackLabel && (
          <span className="text-caption font-medium text-muted-foreground">
            {fallbackLabel}
          </span>
        )}
        <p className="text-caption text-muted-foreground">{t(($) => $.detail.not_found)}</p>
      </div>
    );
  }

  return <IssueHoverCardDetails issue={issue} />;
}

/** One `dt`/`dd` pair of the property list. */
function PropertyRow({
  icon: Icon,
  label,
  children,
}: {
  icon: IconComponent;
  label: string;
  children: ReactNode;
}) {
  return (
    <>
      <dt className="flex items-center gap-1.5 whitespace-nowrap text-caption text-muted-foreground">
        <Icon className="size-3.5 shrink-0" aria-hidden="true" />
        {label}
      </dt>
      <dd className="flex min-h-5 min-w-0 items-center gap-1.5 text-caption text-foreground">
        {children}
      </dd>
    </>
  );
}

/**
 * The PR worth showing when there is room for one: the first still-open one,
 * since a merged or closed PR says less about where the issue stands.
 */
function leadPullRequest(prs: GitHubPullRequest[]): GitHubPullRequest | undefined {
  return prs.find((pr) => pr.state === "open" || pr.state === "draft") ?? prs[0];
}

/**
 * The loaded card. Split from IssueHoverCardBody so the queries keyed on the
 * issue's own fields (project, parent) mount only after the detail settles.
 */
function IssueHoverCardDetails({ issue }: { issue: Issue }) {
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const { colorOf: statusColorOf, iconOf: statusIconOf } = useIssueStatuses(wsId);
  const resolveStatusLabel = useStatusLabel(wsId);

  // One workspace-wide progress snapshot shared with the issues list and issue
  // detail, not a per-issue children fetch: opening a card reuses the cache.
  const { data: childProgress } = useQuery(childIssueProgressOptions(wsId));
  // The same workspace task snapshot the board's activity badge observes.
  const selectTasks = useCallback(
    (snapshot: AgentTask[]) => selectIssueTasks(snapshot, issue.id),
    [issue.id],
  );
  const { data: tasks } = useQuery({ ...agentTaskSnapshotOptions(wsId), select: selectTasks });
  const { data: projects } = useQuery({
    ...projectListOptions(wsId),
    enabled: !!issue.project_id,
  });
  const parentId = issue.parent_issue_id;
  const { data: parent } = useQuery({
    ...issueDetailOptions(wsId, parentId ?? ""),
    enabled: !!parentId,
  });
  const { data: pullRequests } = useQuery(issuePullRequestsOptions(issue.id));

  const preview = issue.description ? descriptionPreview(issue.description) : "";
  // Board cards and list rows render the glyph into a fixed grid slot, so the
  // "none" dash holds a column there. This card has no column to hold, leaving
  // the dash a mark carrying no information in an already dense popup.
  const hasPriority = !!issue.priority && issue.priority !== "none";
  const hasAssignee = !!issue.assignee_type && !!issue.assignee_id;
  // The badge sits beside the assignee, so it only counts that agent's own
  // runs — another agent reviewing the issue is not the assignee working.
  const isAssigneeTask = (task: AgentTask) =>
    issue.assignee_type === "agent" && task.agent_id === issue.assignee_id;
  const isWorking = !!tasks?.running.some(isAssigneeTask);
  const isQueued = !isWorking && !!tasks?.queued.some(isAssigneeTask);
  const project = issue.project_id
    ? projects?.find((p) => p.id === issue.project_id)
    : undefined;
  const labels = issue.labels ?? [];
  const progress = childProgress?.get(issue.id);
  const hasProgress = !!progress && progress.total > 0;
  const prs = pullRequests?.pull_requests ?? [];
  const leadPr = leadPullRequest(prs);
  const isOverdue = !!issue.due_date && isPastDateOnly(issue.due_date);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-1.5 text-caption">
          <StatusIcon
            status={issue.status}
            category={issueStatusCategory(issue) ?? undefined}
            color={statusColorOf(issue.status)}
            icon={statusIconOf(issue.status)}
            className="h-3.5 w-3.5 shrink-0"
          />
          <span className="shrink-0 font-medium text-muted-foreground">{issue.identifier}</span>
          <span aria-hidden="true" className="text-faint-foreground">·</span>
          <span className="truncate text-muted-foreground">{resolveStatusLabel(issue.status)}</span>
        </div>
        {hasPriority && (
          <span className="flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
            <PriorityIcon priority={issue.priority} />
            {t(($) => $.priority[issue.priority])}
          </span>
        )}
      </div>

      {/* The full title is the point of the card, so it never truncates — but
          an unbroken token (a pasted URL) would blow past the card width
          without a break opportunity. */}
      <p className="text-body font-medium text-foreground break-words">{issue.title}</p>

      {preview && <p className="text-caption text-muted-foreground line-clamp-2">{preview}</p>}

      <dl className="-mx-3 grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 gap-y-1.5 border-t border-border px-3 pt-2.5">
        <PropertyRow icon={UserRound} label={t(($) => $.detail.prop_assignee)}>
          {hasAssignee ? (
            <>
              {/* This is already a hover card, and the agent live-peek variant
                  would fire its own request — so no nested card, no link. */}
              <ActorAvatar
                actorType={issue.assignee_type!}
                actorId={issue.assignee_id!}
                size="xs"
                enableHoverCard={false}
                profileLink={false}
                className="shrink-0"
              />
              <span className="min-w-0 truncate">
                {getActorName(issue.assignee_type!, issue.assignee_id!)}
              </span>
              {(isWorking || isQueued) && (
                <ShimmerText active={isWorking} className="shrink-0 text-caption text-muted-foreground">
                  {isWorking
                    ? t(($) => $.agent_activity.status_running)
                    : t(($) => $.agent_activity.status_queued)}
                </ShimmerText>
              )}
            </>
          ) : (
            <span className="text-muted-foreground">{t(($) => $.actions.unassigned)}</span>
          )}
        </PropertyRow>

        {project && (
          <PropertyRow icon={Box} label={t(($) => $.detail.prop_project)}>
            <span className="min-w-0 truncate">{project.title}</span>
          </PropertyRow>
        )}

        {parent && (
          <PropertyRow icon={CornerLeftUp} label={t(($) => $.detail.section_parent_issue)}>
            <span className="shrink-0 font-medium text-muted-foreground">{parent.identifier}</span>
            <span className="min-w-0 truncate">{parent.title}</span>
          </PropertyRow>
        )}

        {labels.length > 0 && (
          <PropertyRow icon={Tag} label={t(($) => $.detail.prop_labels)}>
            <span className="flex min-w-0 items-center gap-1 overflow-hidden">
              {labels.slice(0, MAX_LABELS).map((label) => (
                <LabelChip key={label.id} label={label} className="py-0 text-micro" />
              ))}
            </span>
            {labels.length > MAX_LABELS && (
              <span className="shrink-0 text-micro text-muted-foreground">
                +{labels.length - MAX_LABELS}
              </span>
            )}
          </PropertyRow>
        )}

        {issue.due_date && (
          <PropertyRow icon={CalendarDays} label={t(($) => $.detail.prop_due_date)}>
            <span className={cn(isOverdue && "text-destructive")}>
              {formatDateOnly(issue.due_date, { month: "short", day: "numeric" }, locale)}
            </span>
          </PropertyRow>
        )}

        {hasProgress && (
          <PropertyRow icon={ListTree} label={t(($) => $.detail.sub_issues_label)}>
            <ProgressRing done={progress.done} total={progress.total} size={12} />
            <span className="font-medium tabular-nums text-muted-foreground">
              {progress.done}/{progress.total}
            </span>
          </PropertyRow>
        )}

        {leadPr && (
          <PropertyRow icon={GitPullRequest} label={t(($) => $.detail.section_pull_requests)}>
            <PullRequestInlineSummary pr={leadPr} />
            {prs.length > 1 && (
              <span className="shrink-0 text-micro text-muted-foreground">+{prs.length - 1}</span>
            )}
          </PropertyRow>
        )}
      </dl>

      <div className="-mx-3 flex min-w-0 items-center gap-1.5 border-t border-border px-3 pt-2.5 text-caption text-muted-foreground">
        <ActorAvatar
          actorType={issue.creator_type}
          actorId={issue.creator_id}
          size="xs"
          enableHoverCard={false}
          profileLink={false}
          className="shrink-0"
        />
        <span className="min-w-0 truncate text-foreground">
          {getActorName(issue.creator_type, issue.creator_id)}
        </span>
        <span className="shrink-0 whitespace-nowrap">
          {t(($) => $.hover_card.created_updated, {
            created: timeAgo(issue.created_at),
            updated: timeAgo(issue.updated_at),
          })}
        </span>
      </div>
    </div>
  );
}

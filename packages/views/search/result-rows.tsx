"use client";

import type { ReactNode } from "react";
import { FileText, MessageSquare } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueStatusCategory } from "@multica/core/issues";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import type {
  AgentAvailability,
} from "@multica/core/agents";
import type {
  Issue,
  ProjectStatus,
  SearchIssueResult,
  SearchProjectResult,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { availabilityConfig } from "../agents/presence";
import { StatusIcon } from "../issues/components";
import { ProjectIcon } from "../projects/components/project-icon";
import { useProjectStatusLabels } from "../projects/components/labels";
import { useT } from "../i18n";
import { HighlightText } from "./highlight-text";
import { ROW_CLASS, RowHint } from "./rows";

function IssueAssigneeAvatar({
  assigneeType,
  assigneeId,
}: {
  assigneeType?: string | null;
  assigneeId?: string | null;
}) {
  if (!assigneeType || !assigneeId) return null;
  return (
    <ActorAvatar
      actorType={assigneeType}
      actorId={assigneeId}
      size="sm"
      profileLink={false}
      className="shrink-0"
    />
  );
}

const SNIPPET_CLASS = "flex items-start gap-2 pl-[26px]";

export function ProjectResultRow({
  project,
  query,
  disabled,
  onSelect,
  trailing,
}: {
  project: SearchProjectResult;
  query: string;
  disabled?: boolean;
  onSelect: () => void;
  trailing?: ReactNode;
}) {
  const projectStatusLabels = useProjectStatusLabels();
  const status = project.status as ProjectStatus;

  return (
    <CommandPrimitive.Item
      value={`project:${project.id}`}
      disabled={disabled}
      onSelect={onSelect}
      className={cn(ROW_CLASS, "flex-col items-stretch gap-0.5")}
    >
      <div className="flex min-h-7 items-center gap-2.5">
        <ProjectIcon project={project} size="md" />
        <span className="min-w-0 flex-1 truncate">
          <HighlightText text={project.title} query={query} />
        </span>
        <span
          className={`shrink-0 text-caption ${PROJECT_STATUS_CONFIG[status]?.color ?? "text-muted-foreground"}`}
        >
          {projectStatusLabels[status] ?? project.status}
        </span>
        {trailing}
      </div>
      {project.match_source === "description" && project.matched_snippet && (
        <div className={SNIPPET_CLASS}>
          <span className="truncate text-caption text-muted-foreground">
            <HighlightText text={project.matched_snippet} query={query} />
          </span>
        </div>
      )}
    </CommandPrimitive.Item>
  );
}

export function IssueResultRow({
  issue,
  query,
  disabled,
  onSelect,
  value,
  trailing,
}: {
  issue: SearchIssueResult | Issue;
  query: string;
  disabled?: boolean;
  onSelect: () => void;
  /** cmdk value; defaults to the issue id. */
  value?: string;
  trailing?: ReactNode;
}) {
  const { colorOf, iconOf } = useIssueStatuses(useWorkspaceId());
  const descriptionSnippet =
    "matched_description_snippet" in issue ? issue.matched_description_snippet : undefined;
  const commentSnippet =
    "matched_comment_snippet" in issue ? issue.matched_comment_snippet : undefined;
  return (
    <CommandPrimitive.Item
      value={value ?? issue.id}
      disabled={disabled}
      onSelect={onSelect}
      className={cn(ROW_CLASS, "flex-col items-stretch gap-0.5")}
    >
      <div className="flex min-h-7 items-center gap-2.5">
        <StatusIcon
          status={issue.status}
          color={colorOf(issue.status)}
          icon={iconOf(issue.status)}
          category={issueStatusCategory(issue) ?? undefined}
          className="size-4 shrink-0"
        />
        <span className="shrink-0 text-caption text-muted-foreground">{issue.identifier}</span>
        <span className="min-w-0 flex-1 truncate">
          <HighlightText text={issue.title} query={query} />
        </span>
        <IssueAssigneeAvatar assigneeType={issue.assignee_type} assigneeId={issue.assignee_id} />
        {trailing}
      </div>
      {descriptionSnippet && (
        <div className={SNIPPET_CLASS}>
          <FileText className="mt-0.5 size-3 shrink-0 text-muted-foreground" />
          <span className="truncate text-caption text-muted-foreground">
            <HighlightText text={descriptionSnippet} query={query} />
          </span>
        </div>
      )}
      {commentSnippet && (
        <div className={SNIPPET_CLASS}>
          <MessageSquare className="mt-0.5 size-3 shrink-0 text-muted-foreground" />
          <span className="truncate text-caption text-muted-foreground">
            <HighlightText text={commentSnippet} query={query} />
          </span>
        </div>
      )}
    </CommandPrimitive.Item>
  );
}

/** "● Online · Claude Code (host)" under or beside an agent's name. */
export function AgentPresenceText({
  availability,
  runtimeLabel,
  className,
}: {
  availability: AgentAvailability | null;
  runtimeLabel?: string | null;
  className?: string;
}) {
  const { t } = useT("agents");
  if (!availability && !runtimeLabel) return null;
  return (
    <span className={cn("flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground", className)}>
      {availability && (
        <>
          <span className={cn("size-1.5 shrink-0 rounded-full", availabilityConfig[availability].dotClass)} />
          <span className="shrink-0">{t(($) => $.availability[availability])}</span>
        </>
      )}
      {availability && runtimeLabel && <span aria-hidden="true">·</span>}
      {runtimeLabel && <span className="truncate">{runtimeLabel}</span>}
    </span>
  );
}

export function ActorRow({
  value,
  actorType,
  actorId,
  name,
  query,
  onSelect,
  disabled,
  detail,
  trailing,
  showStatusDot,
}: {
  value: string;
  actorType: "member" | "agent" | "squad";
  actorId: string;
  name: string;
  query: string;
  onSelect: () => void;
  disabled?: boolean;
  detail?: ReactNode;
  trailing?: ReactNode;
  showStatusDot?: boolean;
}) {
  return (
    <CommandPrimitive.Item value={value} disabled={disabled} onSelect={onSelect} className={ROW_CLASS}>
      <ActorAvatar
        actorType={actorType}
        actorId={actorId}
        size="sm"
        profileLink={false}
        showStatusDot={showStatusDot}
        className="shrink-0"
      />
      <span className="shrink-0 truncate">
        <HighlightText text={name} query={query} />
      </span>
      {detail && <span className="min-w-0 flex-1 truncate">{detail}</span>}
      {!detail && <span className="flex-1" />}
      {trailing}
    </CommandPrimitive.Item>
  );
}

/** A generic object row: icon, title, optional detail, type label. */
export function ObjectRow({
  value,
  icon,
  title,
  query,
  detail,
  typeLabel,
  onSelect,
  disabled,
}: {
  value: string;
  icon: ReactNode;
  title: string;
  query: string;
  detail?: ReactNode;
  typeLabel?: string;
  onSelect: () => void;
  disabled?: boolean;
}) {
  return (
    <CommandPrimitive.Item value={value} disabled={disabled} onSelect={onSelect} className={ROW_CLASS}>
      {icon}
      <span className="min-w-0 shrink truncate">
        <HighlightText text={title} query={query} />
      </span>
      {detail && <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{detail}</span>}
      {!detail && <span className="flex-1" />}
      {typeLabel && <RowHint>{typeLabel}</RowHint>}
    </CommandPrimitive.Item>
  );
}

"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "@multica/ui/icons";
import type { Project } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { ProjectActivityFeed } from "./project-activity";
import { ProjectBreakdown, ProjectProgressSection, type BreakdownFilter } from "./project-progress-section";

const ACTIVITY_DIGEST = 5;
const ISSUES_PER_AGENT = 3;

function InsightSection({
  title,
  count,
  action,
  children,
}: {
  title: string;
  count?: number;
  action?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <section>
      <div className="mb-2 flex items-center gap-1">
        <button
          type="button"
          aria-expanded={open}
          className={`flex min-w-0 items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => setOpen(!open)}
        >
          {title}
          {count !== undefined && <span className="tabular-nums text-muted-foreground">{count}</span>}
          <ChevronRight
            className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
          />
        </button>
        {open && action && <div className="ml-auto">{action}</div>}
      </div>
      {open && <div className="pl-2">{children}</div>}
    </section>
  );
}

/**
 * The project sidebar, the same on every tab: how far along it is, where the
 * work sits, which agents are on it now, and what just changed. Properties
 * live on the Overview, so nothing here duplicates them.
 */
export function ProjectInsights({
  project,
  onFilter,
  onShowActivity,
}: {
  project: Project;
  onFilter: (filter: BreakdownFilter) => void;
  onShowActivity: () => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: workingAgents } = useQuery(
    workspaceWorkingAgentsOptions(wsId, "issue", undefined, undefined, project.id),
  );

  return (
    <div className="space-y-5" data-testid="project-insights">
      <InsightSection title={t(($) => $.detail.section_progress)}>
        <ProjectProgressSection project={project} />
      </InsightSection>

      {project.issue_count > 0 && (
        <InsightSection title={t(($) => $.detail.section_breakdown)}>
          <ProjectBreakdown project={project} onFilter={onFilter} />
        </InsightSection>
      )}

      <InsightSection title={t(($) => $.detail.section_working)} count={workingAgents?.length}>
        {workingAgents && workingAgents.length > 0 ? (
          <ul className="space-y-3">
            {workingAgents.map((agent) => (
              <WorkingAgentRow key={agent.id} agentId={agent.id} issueIds={agent.issue_ids} />
            ))}
          </ul>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.detail.working_empty)}</p>
        )}
      </InsightSection>

      <InsightSection
        title={t(($) => $.detail.section_activity)}
        action={
          <button
            type="button"
            className="rounded-md px-1.5 py-0.5 text-caption text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
            onClick={onShowActivity}
          >
            {t(($) => $.detail.activity.view_all)}
          </button>
        }
      >
        <ProjectActivityFeed projectId={project.id} limit={ACTIVITY_DIGEST} dense />
      </InsightSection>
    </div>
  );
}

function WorkingAgentRow({ agentId, issueIds }: { agentId: string; issueIds: string[] }) {
  const { getActorName } = useActorName();
  return (
    <li className="space-y-1">
      <div className="flex items-center gap-2 text-caption">
        <ActorAvatar actorType="agent" actorId={agentId} size="sm" showStatusDot />
        <span className="min-w-0 flex-1 truncate font-medium">{getActorName("agent", agentId)}</span>
      </div>
      <ul className="space-y-0.5 pl-7">
        {issueIds.slice(0, ISSUES_PER_AGENT).map((id) => (
          <WorkingIssueLink key={id} issueId={id} />
        ))}
      </ul>
    </li>
  );
}

function WorkingIssueLink({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  return (
    <li className="min-w-0">
      <AppLink
        href={paths.issueDetail(issueId)}
        className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground hover:text-foreground"
      >
        <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-success" aria-hidden="true" />
        {issue ? (
          <>
            <span className="shrink-0 tabular-nums">{issue.identifier}</span>
            <span className="min-w-0 truncate">{issue.title}</span>
          </>
        ) : (
          <span className="h-3 w-32 rounded bg-muted" />
        )}
      </AppLink>
    </li>
  );
}

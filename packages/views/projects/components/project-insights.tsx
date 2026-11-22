"use client";

import { useQuery } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { ObjectPanelSection } from "../../layout/object-page";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { ProjectActivityFeed } from "./project-activity";
import { ProjectBreakdown, ProjectProgressSection, type BreakdownFilter } from "./project-progress-section";

const ACTIVITY_DIGEST = 5;
const ISSUES_PER_AGENT = 3;

/**
 * The panel sections under Properties, the same on every tab: how far along
 * the project is, where the work sits, which agents are on it now, and what
 * just changed.
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
      <ObjectPanelSection title={t(($) => $.detail.section_progress)}>
        <ProjectProgressSection project={project} />
      </ObjectPanelSection>

      {project.issue_count > 0 && (
        <ObjectPanelSection title={t(($) => $.detail.section_breakdown)}>
          <ProjectBreakdown project={project} onFilter={onFilter} />
        </ObjectPanelSection>
      )}

      <ObjectPanelSection
        title={
          <>
            {t(($) => $.detail.section_working)}
            {workingAgents && workingAgents.length > 0 && (
              <span className="tabular-nums text-muted-foreground">{workingAgents.length}</span>
            )}
          </>
        }
      >
        {workingAgents && workingAgents.length > 0 ? (
          <ul className="space-y-3">
            {workingAgents.map((agent) => (
              <WorkingAgentRow key={agent.id} agentId={agent.id} issueIds={agent.issue_ids} />
            ))}
          </ul>
        ) : (
          <p className="text-caption text-muted-foreground">{t(($) => $.detail.working_empty)}</p>
        )}
      </ObjectPanelSection>

      <ObjectPanelSection
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
      </ObjectPanelSection>
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
        href={paths.issueDetail(issue ? issueRouteRef(issue) : issueRouteRef({ id: issueId, identifier: null }))}
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

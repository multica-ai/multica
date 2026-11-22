"use client";

import type { Project } from "@multica/core/types";
import { ObjectPanelSection } from "../../layout/object-page";
import { useT } from "../../i18n";
import { ProjectActivityFeed } from "./project-activity";
import { ProjectBreakdown, ProjectProgressSection, type BreakdownFilter } from "./project-progress-section";
import { getProjectProgress } from "./project-issue-metrics";

const ACTIVITY_DIGEST = 5;

/**
 * The panel sections under Properties, the same on every tab: how far along
 * the project is, where the work sits, and what just changed.
 */
export function ProjectInsights({
  project,
  onFilter,
  onShowActivity,
  readOnly = false,
}: {
  project: Project;
  onFilter: (filter: BreakdownFilter) => void;
  onShowActivity: () => void;
  readOnly?: boolean;
}) {
  const { t } = useT("projects");

  return (
    <div className="space-y-5" data-testid="project-insights">
      <ObjectPanelSection title={t(($) => $.detail.section_progress)}>
        <ProjectProgressSection project={project} readOnly={readOnly} />
      </ObjectPanelSection>

      {getProjectProgress(project).scope > 0 && (
        <ObjectPanelSection title={t(($) => $.detail.section_breakdown)}>
          <ProjectBreakdown project={project} onFilter={onFilter} />
        </ObjectPanelSection>
      )}

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

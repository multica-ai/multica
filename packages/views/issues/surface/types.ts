import type { IssueScope } from "@multica/core/issues/surface/scope";
import type { CreateIssueRequest } from "@multica/core/types";
import type { ViewMode } from "@multica/core/issues/stores/view-store";

export type IssueCreateDefaults = Partial<
  Omit<
    CreateIssueRequest,
    "assignee_type" | "assignee_id" | "parent_issue_id" | "project_id"
  >
> & {
  assignee_type?: CreateIssueRequest["assignee_type"] | null;
  assignee_id?: string | null;
  parent_issue_id?: string | null;
  /** Display-only context for the create dialog while the parent query loads. */
  parent_issue_identifier?: string;
  project_id?: string | null;
};

export type IssueSurfaceMode = Extract<
  ViewMode,
  | "board"
  | "list"
  | "table"
  | "swimlane"
  | "gantt"
  | "plan_document"
  | "plan_pipeline"
  | "plan_coverage"
>;

/** The three Plan view modes, as a set surfaces opt into together. */
export type PlanViewMode = Extract<
  IssueSurfaceMode,
  "plan_document" | "plan_pipeline" | "plan_coverage"
>;

export const PLAN_VIEW_MODES: readonly PlanViewMode[] = [
  "plan_document",
  "plan_pipeline",
  "plan_coverage",
];

export function isPlanViewMode(mode: IssueSurfaceMode): mode is PlanViewMode {
  return (PLAN_VIEW_MODES as readonly IssueSurfaceMode[]).includes(mode);
}

export interface IssueSurfaceProps {
  scope: IssueScope;
  modes: IssueSurfaceMode[];
  surfaceKey?: string;
  createDefaults?: IssueCreateDefaults;
  /** Server-owned membership search shared by non-Table issue surfaces. */
  search?: string;
}

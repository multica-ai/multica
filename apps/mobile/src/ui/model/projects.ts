import type { Project, ProjectStatus } from "@multica/core/types";
import type { AvatarActor } from "../components/Avatar";
import type { Strings } from "../strings";
import type { Palette } from "../theme";
import { formatDay } from "./dates";
import type { ActorResolver } from "./people";

export const PROJECT_STATUSES: readonly ProjectStatus[] = ["in_progress", "planned", "paused", "completed", "cancelled"];

export function projectStatusColor(palette: Palette, status: string): string {
  switch (status) {
    case "in_progress":
      return palette.warning;
    case "completed":
      return palette.info;
    case "paused":
      return palette.label2;
    default:
      return palette.label3;
  }
}

export interface ProjectRowModel {
  id: string;
  icon: string | null;
  title: string;
  status: ProjectStatus;
  lead: AvatarActor | null;
  done: number;
  total: number;
  /** Already formatted, e.g. "Nov 30". */
  due: string | null;
}

export function projectRowModel(project: Project, resolveActor: ActorResolver, strings: Strings): ProjectRowModel {
  return {
    id: project.id,
    icon: project.icon,
    title: project.title,
    status: project.status,
    lead: project.lead_type && project.lead_id ? resolveActor(project.lead_type, project.lead_id) : null,
    done: project.done_count,
    total: project.issue_count,
    due: project.due_date ? formatDay(project.due_date, strings) : null,
  };
}

export interface ProjectSection {
  status: ProjectStatus;
  projects: ProjectRowModel[];
}

/** Projects under their status, in the order work moves; newest first within one. */
export function projectSections(projects: readonly Project[], resolveActor: ActorResolver, strings: Strings): ProjectSection[] {
  return PROJECT_STATUSES.map((status) => ({
    status,
    projects: projects
      .filter((project) => project.status === status)
      .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
      .map((project) => projectRowModel(project, resolveActor, strings)),
  })).filter((section) => section.projects.length > 0);
}

/** "Nov 1 – Nov 30", "Due Nov 30", or null when neither date is set. */
export function projectDates(project: Pick<Project, "start_date" | "due_date">, strings: Strings): string | null {
  const start = project.start_date ? formatDay(project.start_date, strings) : null;
  const due = project.due_date ? formatDay(project.due_date, strings) : null;
  if (start && due) return strings.when.range(start, due);
  if (due) return strings.project.dueOn(due);
  return start;
}

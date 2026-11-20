import type { Project } from "../types";

/** An archived project stays readable but is no longer offered as a choice. */
export function isProjectArchived(project: Pick<Project, "archived_at"> | null | undefined): boolean {
  return !!project?.archived_at;
}

/**
 * The projects a picker should offer: everything not archived, plus the
 * currently selected one so an issue already in an archived project still
 * shows its value.
 */
export function selectableProjects<T extends Pick<Project, "id" | "archived_at">>(
  projects: readonly T[],
  selectedId?: string | null,
): T[] {
  return projects.filter((p) => !isProjectArchived(p) || p.id === selectedId);
}

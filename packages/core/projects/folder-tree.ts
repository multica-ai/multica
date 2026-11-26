import { NO_PROJECT_ORDER_ID } from "../issues/sidebar-list";
import type { SidebarProjectSettings } from "../issues/stores/sidebar-tree-store";
import type { Project } from "../types";

/**
 * A project folder as the sidebar draws it. Folders come from an extension
 * (packages/ee); without one the sidebar has none.
 */
export interface SidebarFolder {
  id: string;
  name: string;
}

/** A folder's key in the sidebar tree's expanded set, apart from list sources. */
export const projectFolderTreeKey = (folderId: string) => `folder:${folderId}`;

export type SidebarProjectRow<P extends Pick<Project, "id">> =
  | { kind: "project"; id: string; project: P }
  | { kind: "folder"; id: string; folder: SidebarFolder; projects: P[] }
  | { kind: "no_project"; id: string };

/**
 * The sidebar's top-level Projects rows: folders, projects outside any
 * folder, and "No project" when shown. `shown` is already filtered and
 * ordered by the section's settings, and a folder keeps its projects in that
 * order. A folder none of the shown projects sit in is left out, unless no
 * project at all sits in it, so a new folder can still be filled.
 *
 * Folders and "No project" go first, folders by name, unless the order is
 * manual: then every row takes its place in `manualOrder`, and a row not in
 * it yet goes first, before the projects it does not list.
 */
export function sidebarProjectRows<P extends Pick<Project, "id">>(
  shown: readonly P[],
  folders: readonly SidebarFolder[],
  folderOf: ReadonlyMap<string, string>,
  settings: Pick<SidebarProjectSettings, "ordering" | "showNoProject">,
  manualOrder: readonly string[],
): SidebarProjectRow<P>[] {
  const byFolder = new Map<string, P[]>(folders.map((folder) => [folder.id, []]));
  const loose: P[] = [];
  for (const project of shown) {
    const folderId = folderOf.get(project.id);
    const inFolder = folderId ? byFolder.get(folderId) : undefined;
    if (inFolder) inFolder.push(project);
    else loose.push(project);
  }
  const filled = new Set(folderOf.values());
  const folderRows: SidebarProjectRow<P>[] = folders
    .filter((folder) => byFolder.get(folder.id)!.length > 0 || !filled.has(folder.id))
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((folder) => ({ kind: "folder", id: folder.id, folder, projects: byFolder.get(folder.id)! }));

  const rows: SidebarProjectRow<P>[] = [
    ...(settings.showNoProject ? [{ kind: "no_project" as const, id: NO_PROJECT_ORDER_ID }] : []),
    ...folderRows,
    ...loose.map((project) => ({ kind: "project" as const, id: project.id, project })),
  ];
  if (settings.ordering !== "manual") return rows;

  const rank = new Map(manualOrder.map((id, i) => [id, i]));
  return rows
    .map((row, i) => ({
      row,
      key: rank.get(row.id) ?? (row.kind === "project" ? manualOrder.length + i : i - rows.length),
    }))
    .sort((a, b) => a.key - b.key)
    .map(({ row }) => row);
}

/**
 * Folders in the order `sidebarProjectRows` lists them, whichever projects
 * are shown, so a folder picker reads like the sidebar.
 */
export function sidebarFolderOrder<F extends SidebarFolder>(
  folders: readonly F[],
  settings: Pick<SidebarProjectSettings, "ordering">,
  manualOrder: readonly string[],
): F[] {
  const byName = [...folders].sort((a, b) => a.name.localeCompare(b.name));
  if (settings.ordering !== "manual") return byName;
  const rank = new Map(manualOrder.map((id, i) => [id, i]));
  return byName
    .map((folder, i) => ({ folder, key: rank.get(folder.id) ?? i - byName.length }))
    .sort((a, b) => a.key - b.key)
    .map(({ folder }) => folder);
}

/** The rows' ids in display order, each folder followed by its projects: the manual order to save. */
export function sidebarProjectRowIds(rows: readonly SidebarProjectRow<Pick<Project, "id">>[]): string[] {
  return rows.flatMap((row) => (row.kind === "folder" ? [row.id, ...row.projects.map((p) => p.id)] : [row.id]));
}

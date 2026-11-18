import type { ComponentType } from "react";
import type { SidebarFolder } from "@multica/core/projects/folder-tree";
import type { Project } from "@multica/core/types";

/**
 * Project folders are a commercial feature (packages/ee). The open-source
 * views only draw what an extension provides: the sidebar tree, the Projects
 * page grouping, and the slots below. Without an extension there are no
 * folders and every surface renders as before.
 */
export interface ProjectFolders {
  /** In display order. */
  folders: readonly SidebarFolder[];
  /** Folder of each filed project the caller can see, by project id. */
  folderOf: ReadonlyMap<string, string>;
  /** Projects may be filed into folders now; false until the folders have loaded. */
  canFile: boolean;
  /** Files a project in a folder, or moves it out with null. Reports failures itself. */
  moveProject: (projectId: string, folderId: string | null) => void;
  labels: {
    /** "Folder", as a grouping. */
    group: string;
    noFolder: string;
    /** Under an expanded folder with no projects. */
    empty: string;
    newProjectIn: (folderName: string) => string;
  };
}

export interface ProjectFoldersExtension {
  useProjectFolders: (wsId: string) => ProjectFolders;
  /** Items after "New project" in the sidebar Projects section's add menu. */
  AddMenuItems: ComponentType;
  /** A folder row's menu, opened at the pointer inside a CollectionRowContext. */
  FolderMenu: ComponentType<{ folder: SidebarFolder }>;
  /** Items in a project's row menu. */
  ProjectMenuItems: ComponentType<{ project: Project }>;
  /** The new-project dialog's folder choice; the dialog files the project after creating it. */
  CreateProjectField: ComponentType<{ value: string | null; onChange: (folderId: string | null) => void }>;
  /** Mounted once per workspace layout, for the extension's dialogs and realtime sync. */
  Host: ComponentType;
}

let extension: ProjectFoldersExtension | null = null;

/** Called once at app start, before the first render. */
export function registerProjectFoldersExtension(next: ProjectFoldersExtension | null): void {
  extension = next;
}

export function getProjectFoldersExtension(): ProjectFoldersExtension | null {
  return extension;
}

const NO_FOLDERS: ProjectFolders = {
  folders: [],
  folderOf: new Map(),
  canFile: false,
  moveProject: () => {},
  labels: { group: "", noFolder: "", empty: "", newProjectIn: () => "" },
};

function useNoProjectFolders(): ProjectFolders {
  return NO_FOLDERS;
}

/** The workspace's folders, or none when no extension provides them. */
export function useProjectFolders(wsId: string): ProjectFolders {
  // Registration happens before the first render and never changes, so
  // every render calls the same hook.
  const useFolders = extension?.useProjectFolders ?? useNoProjectFolders;
  return useFolders(wsId);
}

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { create } from "zustand";
import type { ProjectFolders } from "@multica/views/extensions";
import { projectFolderListOptions, useFileProject, type ProjectFolder } from "./data";
import { useFoldersT } from "./i18n";

const NOT_FILED: ReadonlyMap<string, string> = new Map();

/** The workspace's folders by name, and where projects are filed. */
export function useProjectFoldersState(wsId: string): ProjectFolders {
  const t = useFoldersT();
  const { data } = useQuery(projectFolderListOptions(wsId));
  const { mutate: file } = useFileProject(wsId);

  const folders = useMemo(
    () => [...(data?.folders ?? [])].sort((a, b) => a.name.localeCompare(b.name)),
    [data?.folders],
  );
  const folderOf = useMemo(() => {
    if (!data?.filed.length) return NOT_FILED;
    const known = new Set(data.folders.map((folder) => folder.id));
    return new Map(data.filed.filter((p) => known.has(p.folder_id)).map((p) => [p.project_id, p.folder_id]));
  }, [data]);
  const moveProject = useCallback(
    (projectId: string, folderId: string | null) => file({ projectId, folderId }),
    [file],
  );
  const labels = useMemo<ProjectFolders["labels"]>(
    () => ({
      group: t("group_label"),
      noFolder: t("no_folder"),
      empty: t("empty"),
      newProjectIn: (name) => t("new_project_in_folder", { name }),
    }),
    [t],
  );
  const canFile = data !== undefined;

  return useMemo(
    () => ({ folders, folderOf, canFile, moveProject, labels }),
    [folders, folderOf, canFile, moveProject, labels],
  );
}

export type FolderDialog =
  | { kind: "create"; fileProjectId?: string }
  | { kind: "rename"; folder: ProjectFolder }
  | { kind: "delete"; folder: ProjectFolder };

interface FolderDialogState {
  dialog: FolderDialog | null;
  open: (dialog: FolderDialog) => void;
  close: () => void;
}

export const useFolderDialogStore = create<FolderDialogState>((set) => ({
  dialog: null,
  open: (dialog) => set({ dialog }),
  close: () => set({ dialog: null }),
}));

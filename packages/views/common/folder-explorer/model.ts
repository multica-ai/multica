export interface FolderExplorerFolder {
  id: string;
  parentId: string | null;
  name: string;
  managed: boolean;
}

export interface FolderExplorerEntry {
  id: string;
  folderIds: readonly string[];
}

export function folderExplorerMap<F extends FolderExplorerFolder>(
  folders: readonly F[],
): ReadonlyMap<string, F> {
  return new Map(folders.map((folder) => [folder.id, folder]));
}

export function directChildFolders<F extends FolderExplorerFolder>(
  folders: readonly F[],
  currentFolderId: string | null,
): F[] {
  const byId = folderExplorerMap(folders);
  return folders
    .filter((folder) => {
      if (folder.parentId === currentFolderId) return true;
      return currentFolderId === null && folder.parentId !== null && !byId.has(folder.parentId);
    })
    .slice()
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function directEntries<E extends FolderExplorerEntry>(
  entries: readonly E[],
  currentFolderId: string | null,
  folders: readonly FolderExplorerFolder[],
): E[] {
  const knownFolderIds = new Set(folders.map((folder) => folder.id));
  return entries.filter((entry) =>
    currentFolderId === null
      ? entry.folderIds.every((folderId) => !knownFolderIds.has(folderId))
      : entry.folderIds.includes(currentFolderId),
  );
}

export function folderPath<F extends FolderExplorerFolder>(
  folders: readonly F[],
  folderId: string,
): F[] {
  const byId = folderExplorerMap(folders);
  const path: F[] = [];
  const visited = new Set<string>();
  let current = byId.get(folderId);
  while (current && !visited.has(current.id)) {
    path.unshift(current);
    visited.add(current.id);
    current = current.parentId ? byId.get(current.parentId) : undefined;
  }
  return path;
}

export function folderPathLabel<F extends FolderExplorerFolder>(
  folders: readonly F[],
  folderId: string,
): string {
  return folderPath(folders, folderId)
    .map((folder) => folder.name)
    .join(" / ");
}

export function entryPathHints(
  folders: readonly FolderExplorerFolder[],
  entry: FolderExplorerEntry,
): string[] {
  return [...new Set(
    entry.folderIds
      .map((folderId) => folderPathLabel(folders, folderId))
      .filter((path) => path.length > 0),
  )].sort((a, b) => a.localeCompare(b));
}

export function isManagedLocation(
  folders: readonly FolderExplorerFolder[],
  folderId: string | null,
): boolean {
  if (folderId === null) return false;
  const byId = folderExplorerMap(folders);
  const visited = new Set<string>();
  let current = byId.get(folderId);
  while (current && !visited.has(current.id)) {
    if (current.managed) return true;
    visited.add(current.id);
    current = current.parentId ? byId.get(current.parentId) : undefined;
  }
  return false;
}

export function folderEntryCounts(
  folders: readonly FolderExplorerFolder[],
  entries: readonly FolderExplorerEntry[],
): ReadonlyMap<string, number> {
  return new Map([...folderEntryIds(folders, entries)].map(([folderId, ids]) => [folderId, ids.size]));
}

/** Deduplicated subtree membership, shared by counts and member previews. */
export function folderEntryIds(
  folders: readonly FolderExplorerFolder[],
  entries: readonly FolderExplorerEntry[],
): ReadonlyMap<string, ReadonlySet<string>> {
  const byId = folderExplorerMap(folders);
  const subtreeEntries = new Map<string, Set<string>>(
    folders.map((folder) => [folder.id, new Set<string>()]),
  );
  for (const entry of entries) {
    for (const folderId of new Set(entry.folderIds)) {
      const visited = new Set<string>();
      let current = byId.get(folderId);
      while (current && !visited.has(current.id)) {
        subtreeEntries.get(current.id)?.add(entry.id);
        visited.add(current.id);
        current = current.parentId ? byId.get(current.parentId) : undefined;
      }
    }
  }
  return subtreeEntries;
}

export type FlattenedFolderExplorerRow<F, E> =
  | {
      kind: "folder";
      key: string;
      folder: F;
      parentFolderId: string | null;
      depth: number;
    }
  | {
      kind: "entry";
      key: string;
      entry: E;
      parentFolderId: string | null;
      depth: number;
    };

export function flattenFolderExplorerRows<
  F extends FolderExplorerFolder,
  E extends FolderExplorerEntry,
>({
  folders,
  entries,
  expandedFolderIds,
  entryKey = (entry) => entry.id,
  entryKeyPrefix = "entry",
}: {
  folders: readonly F[];
  entries: readonly E[];
  expandedFolderIds: ReadonlySet<string>;
  entryKey?: (entry: E) => string;
  entryKeyPrefix?: string;
}): FlattenedFolderExplorerRow<F, E>[] {
  const rows: FlattenedFolderExplorerRow<F, E>[] = [];

  const visit = (parentFolderId: string | null, depth: number, ancestors: ReadonlySet<string>) => {
    for (const folder of directChildFolders(folders, parentFolderId)) {
      if (ancestors.has(folder.id)) continue;
      rows.push({
        kind: "folder",
        key: `folder-${folder.id}`,
        folder,
        parentFolderId,
        depth,
      });
      if (expandedFolderIds.has(folder.id)) {
        visit(folder.id, depth + 1, new Set([...ancestors, folder.id]));
      }
    }
    for (const entry of directEntries(entries, parentFolderId, folders)) {
      rows.push({
        kind: "entry",
        key: `${entryKeyPrefix}-${parentFolderId ?? "root"}-${entryKey(entry)}`,
        entry,
        parentFolderId,
        depth,
      });
    }
  };

  visit(null, 0, new Set());
  return rows;
}

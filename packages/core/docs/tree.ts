import type { DocFolder } from "../types";

/** Folders from the root down to `folderId` (inclusive). */
export function folderAncestry(
  folders: readonly DocFolder[],
  folderId: string | null,
): DocFolder[] {
  const byId = new Map(folders.map((f) => [f.id, f]));
  const chain: DocFolder[] = [];
  let cur = folderId ? byId.get(folderId) : undefined;
  while (cur && chain.length <= folders.length) {
    chain.unshift(cur);
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return chain;
}

/** Whether `folderId` may move under `parentId` without becoming its own ancestor. */
export function canMoveFolder(
  folders: readonly DocFolder[],
  folderId: string,
  parentId: string | null,
): boolean {
  if (parentId === null) return true;
  return !folderAncestry(folders, parentId).some((f) => f.id === folderId);
}

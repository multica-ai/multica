import type { DocFolder, DocSummary, DocTree } from "../types";

/** A folder or doc in the knowledge base tree, children sorted folders-first. */
export type DocTreeNode =
  | { kind: "folder"; folder: DocFolder; children: DocTreeNode[]; unread: number }
  | { kind: "doc"; doc: DocSummary };

const byTitle = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: "base", numeric: true });

/**
 * Nests the flat folder/doc lists. Items whose parent is missing (deleted by
 * someone else between reads) surface at the root rather than vanishing.
 */
export function buildDocTree(tree: DocTree): DocTreeNode[] {
  const folderIds = new Set(tree.folders.map((f) => f.id));
  const childFolders = new Map<string | null, DocFolder[]>();
  for (const f of tree.folders) {
    const parent = f.parent_id && folderIds.has(f.parent_id) && f.parent_id !== f.id ? f.parent_id : null;
    childFolders.set(parent, [...(childFolders.get(parent) ?? []), f]);
  }
  const childDocs = new Map<string | null, DocSummary[]>();
  for (const d of tree.docs) {
    const parent = d.folder_id && folderIds.has(d.folder_id) ? d.folder_id : null;
    childDocs.set(parent, [...(childDocs.get(parent) ?? []), d]);
  }

  const visited = new Set<string>();
  const build = (parent: string | null): DocTreeNode[] => {
    const folders = [...(childFolders.get(parent) ?? [])].sort((a, b) => byTitle(a.title, b.title));
    const docs = [...(childDocs.get(parent) ?? [])].sort((a, b) => byTitle(a.title, b.title));
    const nodes: DocTreeNode[] = [];
    for (const folder of folders) {
      if (visited.has(folder.id)) continue;
      visited.add(folder.id);
      const children = build(folder.id);
      nodes.push({ kind: "folder", folder, children, unread: countUnread(children) });
    }
    for (const doc of docs) nodes.push({ kind: "doc", doc });
    return nodes;
  };
  return build(null);
}

function countUnread(nodes: DocTreeNode[]): number {
  let n = 0;
  for (const node of nodes) n += node.kind === "doc" ? (node.doc.unread ? 1 : 0) : node.unread;
  return n;
}

/** Folders from the root down to `folderId` (inclusive). */
export function folderAncestry(folders: readonly DocFolder[], folderId: string | null): DocFolder[] {
  const byId = new Map(folders.map((f) => [f.id, f]));
  const chain: DocFolder[] = [];
  let cur = folderId ? byId.get(folderId) : undefined;
  while (cur && chain.length <= folders.length) {
    chain.unshift(cur);
    cur = cur.parent_id ? byId.get(cur.parent_id) : undefined;
  }
  return chain;
}

/** Whether `folderId` may move under `parentId` without becoming its own ancestor. */
export function canMoveFolder(folders: readonly DocFolder[], folderId: string, parentId: string | null): boolean {
  if (parentId === null) return true;
  return !folderAncestry(folders, parentId).some((f) => f.id === folderId);
}

/** Direct children of a folder (or of the root when null). */
export function folderChildren(tree: DocTree, folderId: string | null): { folders: DocFolder[]; docs: DocSummary[] } {
  return {
    folders: tree.folders
      .filter((f) => (f.parent_id ?? null) === folderId)
      .sort((a, b) => byTitle(a.title, b.title)),
    docs: tree.docs.filter((d) => (d.folder_id ?? null) === folderId).sort((a, b) => byTitle(a.title, b.title)),
  };
}

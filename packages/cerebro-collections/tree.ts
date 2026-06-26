// Pure tree helpers for the Collections page. The backend hands us a flat list
// of folders, each carrying parent_id (null = root); these turn that list into
// the nested shape the tree renders, with no React or API coupling so the
// logic is unit-testable on its own.

import type { CollectionFolder } from "./api";

export interface FolderNode extends CollectionFolder {
  depth: number;
  children: FolderNode[];
}

// Build a forest from a flat folder list. Roots are folders whose parent_id is
// null OR points at a folder not present in this list (e.g. a parent on another
// surface, or one the caller filtered out) — those are surfaced at the top
// rather than silently dropped. Children are sorted by name at every level, and
// a parent_id cycle can never produce an infinite tree because each folder is
// attached exactly once via a visited guard.
export function buildFolderTree(folders: CollectionFolder[]): FolderNode[] {
  const byId = new Map<string, FolderNode>();
  for (const f of folders) {
    byId.set(f.id, { ...f, depth: 0, children: [] });
  }

  const roots: FolderNode[] = [];
  for (const node of byId.values()) {
    const parent =
      node.parent_id !== null ? byId.get(node.parent_id) : undefined;
    if (parent && parent.id !== node.id) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  const byName = (a: FolderNode, b: FolderNode) =>
    a.name.localeCompare(b.name);
  const assignDepth = (nodes: FolderNode[], depth: number) => {
    nodes.sort(byName);
    for (const n of nodes) {
      n.depth = depth;
      assignDepth(n.children, depth + 1);
    }
  };
  assignDepth(roots, 0);
  return roots;
}

// Flatten a forest into a depth-first ordered list — the order the tree renders
// top-to-bottom. Used both for rendering and for offering valid move targets.
export function flattenFolderTree(nodes: FolderNode[]): FolderNode[] {
  const out: FolderNode[] = [];
  const walk = (list: FolderNode[]) => {
    for (const n of list) {
      out.push(n);
      walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

// Collect the id of a folder and every descendant. A folder may not be moved
// into itself or any of its own descendants (that would orphan a subtree /
// create a cycle), so the move menu excludes these targets.
export function collectSubtreeIds(node: FolderNode): Set<string> {
  const ids = new Set<string>();
  const walk = (n: FolderNode) => {
    ids.add(n.id);
    n.children.forEach(walk);
  };
  walk(node);
  return ids;
}

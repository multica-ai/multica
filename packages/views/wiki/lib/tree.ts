import type { WikiPageSummary } from "@multica/core/types";

export interface WikiPageTreeNode extends WikiPageSummary {
  children: WikiPageTreeNode[];
}

export function buildWikiTree(pages: WikiPageSummary[]): WikiPageTreeNode[] {
  const byId = new Map<string, WikiPageTreeNode>();
  for (const page of pages) {
    byId.set(page.id, { ...page, children: [] });
  }

  const roots: WikiPageTreeNode[] = [];
  for (const node of byId.values()) {
    if (node.parent_id && byId.has(node.parent_id)) {
      byId.get(node.parent_id)?.children.push(node);
    } else {
      roots.push(node);
    }
  }

  const sortNodes = (nodes: WikiPageTreeNode[]) => {
    nodes.sort((a, b) => a.position - b.position || a.created_at.localeCompare(b.created_at));
    nodes.forEach((node) => sortNodes(node.children));
  };
  sortNodes(roots);
  return roots;
}

export function flattenWikiTree(nodes: WikiPageTreeNode[]): WikiPageTreeNode[] {
  const out: WikiPageTreeNode[] = [];
  const visit = (node: WikiPageTreeNode) => {
    out.push(node);
    node.children.forEach(visit);
  };
  nodes.forEach(visit);
  return out;
}

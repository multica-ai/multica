import type { Issue, IssueDependency, IssueTableQuerySpec, Project } from "@multica/core/types";

/** Keep the full tree while the map highlights search matches locally. */
export function mindmapQuery(serverQuery: IssueTableQuerySpec): IssueTableQuerySpec {
  const { search: _search, ...query } = serverQuery;
  return { ...query, filters: { ...query.filters, include_sub_issues: true } };
}

export const MINDMAP_CARD_WIDTH = 240;
export const MINDMAP_CARD_HEIGHT = 68;
const COLUMN_GAP = 88;
const ROW_GAP = 30;
const PADDING = 80;

export type MindmapNode = {
  key: string;
  kind: "project" | "issue";
  project?: Project;
  issue?: Issue;
  label: string;
  x: number;
  y: number;
  depth: number;
  parentKey: string | null;
  childCount: number;
  collapsed: boolean;
};

export type MindmapEdge = { from: string; to: string };

export type MindmapLayout = {
  nodes: MindmapNode[];
  hierarchyEdges: MindmapEdge[];
  dependencyEdges: MindmapEdge[];
  hiddenDependencyCount: Map<string, number>;
  width: number;
  height: number;
};

/** Normalize the legacy relationship vocabulary into prerequisite → blocked. */
export function dependencyDirection(row: IssueDependency): MindmapEdge | null {
  if (row.type === "blocked_by") return { from: row.depends_on_issue_id, to: row.issue_id };
  if (row.type === "blocks") return { from: row.issue_id, to: row.depends_on_issue_id };
  return null;
}

export function buildMindmapLayout(
  issues: Issue[],
  projects: Project[],
  dependencies: IssueDependency[],
  expanded: ReadonlyMap<string, boolean>,
): MindmapLayout {
  const byId = new Map(issues.map((issue) => [issue.id, issue]));
  const children = new Map<string, Issue[]>();
  const roots = new Map<string, Issue[]>();
  const validParent = (issue: Issue): string | null => {
    const parent = issue.parent_issue_id && byId.get(issue.parent_issue_id);
    if (!parent || parent.project_id !== issue.project_id) return null;
    const seen = new Set([issue.id]);
    let cursor: Issue | undefined = parent;
    while (cursor) {
      if (seen.has(cursor.id)) return null;
      seen.add(cursor.id);
      cursor = cursor.parent_issue_id ? byId.get(cursor.parent_issue_id) : undefined;
    }
    return parent.id;
  };
  for (const issue of issues) {
    const parent = validParent(issue);
    const bucket = parent ? children : roots;
    const key = parent ?? issue.project_id ?? "unassigned";
    const rows = bucket.get(key) ?? [];
    rows.push(issue);
    bucket.set(key, rows);
  }
  const order = (a: Issue, b: Issue) => a.position - b.position || a.number - b.number;
  for (const rows of [...children.values(), ...roots.values()]) rows.sort(order);

  const nodes: MindmapNode[] = [];
  const hierarchyEdges: MindmapEdge[] = [];
  let nextY = PADDING;
  let maxDepth = 0;
  const addIssue = (issue: Issue, parentKey: string, depth: number): MindmapNode => {
    const key = `issue:${issue.id}`;
    const offspring = children.get(issue.id) ?? [];
    const collapsed = expanded.get(key) === false || (depth >= 2 && expanded.get(key) !== true);
    const node: MindmapNode = {
      key, kind: "issue", issue, label: issue.title,
      x: PADDING + depth * (MINDMAP_CARD_WIDTH + COLUMN_GAP), y: 0,
      depth, parentKey, childCount: offspring.length, collapsed,
    };
    nodes.push(node);
    hierarchyEdges.push({ from: parentKey, to: key });
    maxDepth = Math.max(maxDepth, depth);
    if (offspring.length && !collapsed) {
      const visibleChildren = offspring.map((child) => addIssue(child, key, depth + 1));
      node.y = (visibleChildren[0]!.y + visibleChildren[visibleChildren.length - 1]!.y) / 2;
    } else {
      node.y = nextY;
      nextY += MINDMAP_CARD_HEIGHT + ROW_GAP;
    }
    return node;
  };

  const projectById = new Map(projects.map((project) => [project.id, project]));
  const projectIds = [...roots.keys()].sort((a, b) => {
    if (a === "unassigned") return 1;
    if (b === "unassigned") return -1;
    return (projectById.get(a)?.title ?? a).localeCompare(projectById.get(b)?.title ?? b);
  });
  for (const projectId of projectIds) {
    const key = `project:${projectId}`;
    const project = projectById.get(projectId);
    const projectRoots = roots.get(projectId) ?? [];
    const collapsed = expanded.get(key) === false;
    const node: MindmapNode = {
      key, kind: "project", project, label: project?.title ?? (projectId === "unassigned" ? "" : projectId),
      x: PADDING, y: 0, depth: 0, parentKey: null,
      childCount: projectRoots.length, collapsed,
    };
    nodes.push(node);
    if (projectRoots.length && !collapsed) {
      const visibleRoots = projectRoots.map((issue) => addIssue(issue, key, 1));
      node.y = (visibleRoots[0]!.y + visibleRoots[visibleRoots.length - 1]!.y) / 2;
    } else {
      node.y = nextY;
      nextY += MINDMAP_CARD_HEIGHT + ROW_GAP;
    }
    nextY += ROW_GAP;
  }

  const visible = new Set(nodes.filter((node) => node.kind === "issue").map((node) => node.issue!.id));
  const hiddenDependencyCount = new Map<string, number>();
  const dependencyEdges: MindmapEdge[] = [];
  const seen = new Set<string>();
  for (const row of dependencies) {
    const edge = dependencyDirection(row);
    if (!edge || edge.from === edge.to) continue;
    const identity = `${edge.from}:${edge.to}`;
    if (seen.has(identity)) continue;
    seen.add(identity);
    if (visible.has(edge.from) && visible.has(edge.to)) {
      dependencyEdges.push({ from: `issue:${edge.from}`, to: `issue:${edge.to}` });
    } else {
      if (visible.has(edge.from)) hiddenDependencyCount.set(edge.from, (hiddenDependencyCount.get(edge.from) ?? 0) + 1);
      if (visible.has(edge.to)) hiddenDependencyCount.set(edge.to, (hiddenDependencyCount.get(edge.to) ?? 0) + 1);
    }
  }
  return {
    nodes, hierarchyEdges, dependencyEdges, hiddenDependencyCount,
    width: PADDING * 2 + (maxDepth + 1) * MINDMAP_CARD_WIDTH + maxDepth * COLUMN_GAP,
    height: Math.max(300, nextY + PADDING),
  };
}

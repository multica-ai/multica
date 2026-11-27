import { trajectorySymbol, type TrajectoryGraph } from "./graph";

export type CurrentExecutionCategory = "running" | "blocked" | "review";
export interface CurrentExecutionWindow {
  total: number;
  unknown: number;
  categories: Record<CurrentExecutionCategory, string[]>;
  focusIssueIds: Set<string>;
  ancestorIssueIds: Set<string>;
}

/** The caller supplies the current list window, before merging historical reads. */
export function deriveCurrentExecution(graph: TrajectoryGraph, matchingIssueIds: ReadonlySet<string>): CurrentExecutionWindow {
  const issues = new Map(graph.nodes.filter(node => node.kind === "issue").map(node => [node.issue_id, node]));
  const result: CurrentExecutionWindow = { total: matchingIssueIds.size, unknown: 0,
    categories: { running: [], blocked: [], review: [] }, focusIssueIds: new Set(), ancestorIssueIds: new Set() };
  for (const id of matchingIssueIds) if (!issues.has(id)) result.unknown++;
  for (const [id, node] of issues) {
    if (!matchingIssueIds.has(id)) continue;
    const meaning = trajectorySymbol(node).meaning;
    if (meaning === "unknown") result.unknown++;
    if (meaning === "running" || meaning === "blocked" || meaning === "review") {
      result.categories[meaning].push(id);
      result.focusIssueIds.add(id);
    }
  }
  for (const id of result.focusIssueIds) {
    const visited = new Set([id]);
    let parent = issues.get(id)?.details.parent_id;
    while (typeof parent === "string" && issues.has(parent) && !visited.has(parent)) {
      visited.add(parent);
      if (!result.focusIssueIds.has(parent)) result.ancestorIssueIds.add(parent);
      parent = issues.get(parent)?.details.parent_id;
    }
  }
  return result;
}

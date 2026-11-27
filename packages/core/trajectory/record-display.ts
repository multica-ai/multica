import type { TrajectoryGraph, TrajectoryNode } from "./graph";

/** All necessary returned ancestors, independent of current execution state. */
export function loadedIssueContextIds(graph: TrajectoryGraph, matchingIds: readonly string[]): Set<string> {
  const issues = new Map(graph.nodes.filter(node => node.kind === "issue").map(node => [node.issue_id, node]));
  const included = new Set(matchingIds.filter(id => issues.has(id)));
  for (const id of [...included]) {
    const visited = new Set([id]);
    let parent = issues.get(id)?.details.parent_id;
    while (typeof parent === "string" && issues.has(parent) && !visited.has(parent)) {
      visited.add(parent); included.add(parent); parent = issues.get(parent)?.details.parent_id;
    }
  }
  return included;
}

/** The newest loaded record by creation time; unknown timestamps never win. */
export function latestLoadedRecord(nodes: readonly TrajectoryNode[]): TrajectoryNode | undefined {
  let latest: TrajectoryNode | undefined, latestAt = -Infinity;
  for (const node of nodes) {
    const at = Date.parse(node.created_at);
    if (Number.isFinite(at) && (at > latestAt || at === latestAt && latest && node.id.localeCompare(latest.id) > 0)) { latest = node; latestAt = at; }
  }
  return latest;
}

/** Positions only returned native Runs; latest never means current execution. */
export function loadedRunPositions(graph: TrajectoryGraph, issueId: string): { runningIds: string[]; latestId: string | null } {
  const runs = graph.nodes.filter(node => node.kind === "run" && node.issue_id === issueId);
  const time = (created: string) => Number.isFinite(Date.parse(created)) ? Date.parse(created) : -Infinity;
  const ordered = [...runs].sort((a, b) => time(a.created_at) - time(b.created_at) || a.id.localeCompare(b.id));
  const timed = ordered.filter(node => Number.isFinite(time(node.created_at)));
  return { runningIds: ordered.filter(node => node.status === "running").map(node => node.id), latestId: timed.at(-1)?.id ?? null };
}

export interface RunConversationTarget {
  /** A reply the run posted, or the comment that started it. */
  relation: "reply" | "trigger";
  commentId: string;
  /** The issue the comment is on, and its identifier when that issue is loaded. */
  issueId: string;
  identifier: string | null;
}

const text = (value: unknown) => typeof value === "string" && value.trim() ? value : null;

/**
 * Where a run's conversation is, among the comments the graph returned. The
 * graph only returns comments the viewer can read, so a missing node means
 * no link: never a guess from time, author or text.
 */
export function runConversationTarget(graph: TrajectoryGraph, run: TrajectoryNode): RunConversationTarget | null {
  if (run.kind !== "run" || !run.run_id) return null;
  const comments = graph.nodes.filter(node => node.kind === "comment" && text(node.details.comment_id));
  const target = (relation: RunConversationTarget["relation"], comment: TrajectoryNode): RunConversationTarget => {
    const issue = graph.nodes.find(node => node.kind === "issue" && node.issue_id === comment.issue_id);
    return { relation, commentId: text(comment.details.comment_id)!, issueId: comment.issue_id, identifier: issue ? text(issue.details.issue_key) : null };
  };
  const replies = comments.filter(node => node.run_id === run.run_id)
    .sort((a, b) => (Date.parse(a.created_at) || 0) - (Date.parse(b.created_at) || 0));
  const reply = replies.at(-1);
  if (reply) return target("reply", reply);
  const triggers = [text(run.details.trigger_comment_id), run.details.trigger_evidence_kind === "comment" ? text(run.details.trigger_evidence_ref_id) : null].filter(Boolean);
  const trigger = comments.find(node => triggers.includes(text(node.details.comment_id)));
  return trigger ? target("trigger", trigger) : null;
}

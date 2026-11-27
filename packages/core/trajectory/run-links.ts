import type { TrajectoryGraph } from "./graph";
import type { VisualScene } from "./visual";

const text = (value: unknown) => typeof value === "string" ? value : "";

/** Reconcile native evidence after the visible Issue scopes have been loaded. */
export function trajectoryRunCalls(graph: TrajectoryGraph) {
  const nodes = new Map(graph.nodes.map(node => [node.id, node]));
  const calls = new Map<string, { from: string; to: string }>();
  for (const target of nodes.values()) {
    if (target.kind !== "run" || !target.run_id || target.id !== `run:${target.run_id}`) continue;
    const proof = target.details.run_call;
    if (!proof?.comment_id) continue;
    const ref = proof.comment_id;
    const comment = nodes.get(`comment:${ref}`);
    const sourceId = text(comment?.details.source_run_id);
    const source = nodes.get(`run:${sourceId}`);
    if (comment?.kind !== "comment" || comment.issue_id !== target.issue_id
      || comment.details.comment_id !== ref || comment.details.author_type !== "agent"
      || !sourceId || comment.run_id !== sourceId || target.details.delegated_from !== sourceId
      || source?.kind !== "run" || source.run_id !== sourceId || source.issue_id !== proof.source_issue_id || source.id === target.id) continue;
    const sourceIssue = nodes.get(`issue:${source.issue_id}`);
    const targetIssue = nodes.get(`issue:${target.issue_id}`);
    if (sourceIssue?.kind !== "issue" || sourceIssue.issue_id !== source.issue_id
      || targetIssue?.kind !== "issue" || targetIssue.issue_id !== target.issue_id) continue;

    // Never use a generic parent/delegation edge, an adjacent timestamp, or
    // the presence of a Run-produced comment alone as proof of a call.
    calls.set(target.id, { from: source.id, to: target.id });
  }
  return [...calls.values()];
}

/** The final rows, not timestamps or tree depth, own the arrow endpoints. */
export function layoutTrajectoryRunCalls(graph: TrajectoryGraph, scene: VisualScene) {
  const anchors = new Map(scene.rows.flatMap(row => {
    const y = row.markerY;
    if (row.kind !== "run" || y === null) return [];
    // Use the last painted vertical segment at the marker, not the row's owner tint.
    const rail = row.segments.findLast(segment => segment.column === row.markerColumn && segment.y1 <= y && y <= segment.y2);
    return [[row.id, { x: row.markerX + row.markerRadius + 1, y: row.top + y, colorIndex: rail?.colorIndex ?? null }] as const];
  }));
  const laneEnds: number[] = [];
  let width = scene.railWidth;
  const links = trajectoryRunCalls(graph).flatMap(call => {
    const source = anchors.get(call.from), target = anchors.get(call.to);
    return source && target ? [{ ...call, source, target, low: Math.min(source.y, target.y), high: Math.max(source.y, target.y) }] : [];
  }).sort((a, b) => a.low - b.low || a.to.localeCompare(b.to)).map(({ from, to, source, target, low, high }) => {
    // Never share a vertical stroke between overlapping call spans.
    const free = laneEnds.findIndex(end => end < low);
    const lane = free < 0 ? laneEnds.length : free;
    laneEnds[lane] = high;
    const x = scene.railWidth + 8 + lane * 12;
    const radius = 6, step = target.y > source.y ? radius : -radius;
    width = Math.max(width, x + 12);
    return { from, to, colorIndex: source.colorIndex,
      start: { x, y: source.y },
      path: `M ${x} ${source.y} V ${target.y - step} Q ${x} ${target.y} ${x - radius} ${target.y} H ${target.x + 6}`,
      tip: `${target.x},${target.y} ${target.x + 7},${target.y - 4} ${target.x + 7},${target.y + 4}`,
    };
  });
  return { links, width };
}

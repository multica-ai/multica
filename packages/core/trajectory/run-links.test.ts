// @vitest-environment node
import { expect, it } from "vitest";
import { sanitizeTrajectoryGraph, TrajectoryGraphSchema, type TrajectoryGraph, type TrajectoryNode } from "./graph";
import { layoutTrajectoryRunCalls, trajectoryRunCalls } from "./run-links";
import { buildTrajectoryVisual } from "./visual";

function fixture() {
  const issue = (id: string): TrajectoryNode => ({ id: `issue:${id}`, kind: "issue", issue_id: id, title: id, status: "todo", created_at: "", details: {} });
  const run = (id: string, issueId: string): TrajectoryNode => ({ id: `run:${id}`, kind: "run", issue_id: issueId, run_id: id, title: id, status: "completed", created_at: "", details: {} });
  const parent = issue("parent"), child = issue("child-issue"), source = run("source", parent.issue_id), target = run("target", child.issue_id);
  const comment: TrajectoryNode = { id: "comment:reply", kind: "comment", issue_id: child.issue_id, run_id: "source", title: "Comment", status: "recorded", created_at: "", details: { comment_id: "reply", author_type: "agent", source_run_id: "source" } };
  target.details = { run_call: { comment_id: "reply", source_issue_id: "parent" }, delegated_from: "source", trigger_comment_id: "reply", trigger_evidence_kind: "comment", trigger_evidence_ref_id: "reply" };
  const graph: TrajectoryGraph = { root_issue_id: "parent", nodes: [parent, child, source, target, comment],
    edges: [{ id: "legacy", kind: "delegation", from: source.id, to: target.id }], findings: [], evidence: [], coverage: { complete: true, truncated: false, warnings: [] } };
  return { graph, source, target, comment, child };
}

it("requires both sides of the agent comment provenance, independently of a graph edge", () => {
  const { graph, source, target, comment } = fixture();
  graph.edges = [];
  expect(trajectoryRunCalls(graph)).toEqual([{ from: source.id, to: target.id }]);
  graph.nodes.push(target);
  expect(trajectoryRunCalls(graph)).toHaveLength(1);
  comment.details.source_run_id = "someone-else";
  expect(trajectoryRunCalls(graph)).toEqual([]);
});

it("ignores malformed optional call proof without dropping the Run or its details", () => {
  const { graph, target } = fixture();
  const nodes = graph.nodes.map(node => node.id === target.id ? { ...node, details: { ...node.details, run_call: { comment_id: "reply", source_issue_id: 5 } } } : node);
  const parsed = TrajectoryGraphSchema.parse({ ...graph, nodes });
  expect(parsed.nodes.find(node => node.id === target.id)?.details.delegated_from).toBe("source");
  expect(trajectoryRunCalls(parsed)).toEqual([]);
});

it("does not conflate an Issue's creation and assignment attribution with a later Run dispatch", () => {
  const { graph, source, target, child } = fixture();
  target.details = { originator_source: "delegation", delegated_from: source.run_id, trigger_evidence_kind: "issue_assignment", trigger_evidence_ref_id: child.issue_id };
  child.details = { creation_run_id: source.run_id, creation_issue_id: source.issue_id };
  expect(trajectoryRunCalls(graph)).toEqual([]);
});

it("does not trust a generic edge, timestamp, or a comment without the server-projected call proof", () => {
  const { graph, target } = fixture();
  delete target.details.run_call;
  target.details.parent_run = "source";
  expect(trajectoryRunCalls(graph)).toEqual([]);
});

it.each(["comment:reply", "run:source", "issue:parent"])("strips cached call proof when %s is denied, even from an own-Issue result", denied => {
  const { graph, target } = fixture();
  const safe = sanitizeTrajectoryGraph({ ...graph, nodes: graph.nodes.filter(node => node.issue_id === target.issue_id) }, new Set([denied]));
  expect(trajectoryRunCalls(safe)).toEqual([]);
  expect(safe.nodes.find(node => node.id === target.id)?.details.run_call).toBeUndefined();
});

it.each(["comment:reply", "issue:parent", "run:source", "issue:child-issue", "run:target"])("does not disclose a call if %s is outside the loaded, authorized scope", excluded => {
  const { graph } = fixture();
  graph.nodes = graph.nodes.filter(node => node.id !== excluded);
  expect(trajectoryRunCalls(graph)).toEqual([]);
});


it.each(["task", "time"] as const)("keeps five overlapping %s arrows on distinct tracks and preserves reverse direction", order => {
  const { graph, source, target, comment } = fixture();
  graph.nodes.splice(2);
  for (let n = 0; n < 5; n++) {
    const id = `source-${n}`, ref = `reply-${n}`;
    // The child Runs appear after their callers in task order but before them
    // in time order, so the arrow must work upwards without reversing endpoints.
    graph.nodes.push({ ...source, id: `run:${id}`, run_id: id, created_at: `2026-11-19T00:1${n}:00Z` },
      { ...target, id: `run:target-${n}`, run_id: `target-${n}`, created_at: `2026-11-19T00:0${n}:00Z`, details: { delegated_from: id, run_call: { comment_id: ref, source_issue_id: "parent" } } },
      { ...comment, id: `comment:${ref}`, run_id: id, details: { comment_id: ref, author_type: "agent", source_run_id: id } });
  }
  graph.nodes.find(node => node.id === `issue:${target.issue_id}`)!.details.parent_id = source.issue_id;
  const expanded = new Set([source.issue_id, target.issue_id]);
  const scene = buildTrajectoryVisual(graph, expanded, expanded, new Set(), order);
  const { links } = layoutTrajectoryRunCalls(graph, scene);
  expect(links).toHaveLength(5);
  expect(links.map(({ start }) => start?.x).sort((a, b) => a - b)).toEqual(
    Array.from({ length: 5 }, (_, lane) => scene.railWidth + 8 + lane * 12));
  for (const link of links) {
    const from = scene.rows.find(row => row.id === link.from)!;
    const to = scene.rows.find(row => row.id === link.to)!;
    expect(link.from).toBe(link.to.replace("target", "source"));
    expect(link.start.y).toBe(from.top + from.markerY!);
    const targetY = to.top + to.markerY!;
    expect(targetY > link.start.y).toBe(order === "task");
    expect(link.path.startsWith(`M ${link.start.x} ${link.start.y} V `)).toBe(true);
    expect(link.path.match(/ [A-Z] /g)).toEqual([" V ", " Q ", " H "]);
    expect(link.path.endsWith(`H ${to.markerX + to.markerRadius + 7}`)).toBe(true);
    expect(link.tip.startsWith(`${to.markerX + to.markerRadius + 1},${targetY} `)).toBe(true);
    expect(from.markerColumn).toBe(0);
    expect(from.colorIndex).not.toBeNull(); // The row tint must not color the spine call.
    expect(link.colorIndex).toBeNull();
  }
});

it.each(["main spine", "colored branch"] as const)("starts on a separate route and uses the drawn %s color", lane => {
  const { graph, source, target, comment } = fixture();
  target.issue_id = source.issue_id;
  comment.issue_id = source.issue_id;
  source.status = "running";
  if (lane === "colored branch") {
    const owner = graph.nodes.find(node => node.id === `issue:${source.issue_id}`)!;
    graph.nodes.push({ ...owner, id: "issue:trunk", issue_id: "trunk", details: {} });
    owner.details.parent_id = "trunk";
  }
  const expanded = new Set([source.issue_id]);
  const scene = buildTrajectoryVisual(graph, expanded, expanded);
  const from = scene.rows.find(row => row.id === source.id)!;
  const to = scene.rows.find(row => row.id === target.id)!;
  const markerY = from.markerY!;
  const rail = from.segments.find(segment => segment.column === from.markerColumn && segment.y1 <= markerY && markerY <= segment.y2);
  expect(from.markerColumn === 0).toBe(lane === "main spine");
  expect(from.markerX).toBe(to.markerX);
  expect(from.colorIndex).not.toBeNull();
  expect(rail?.colorIndex).toBe(lane === "main spine" ? null : from.colorIndex);
  const { links } = layoutTrajectoryRunCalls(graph, scene);
  expect(links).toHaveLength(1);
  expect(links[0]).toMatchObject({
    from: source.id, to: target.id, colorIndex: rail?.colorIndex,
    start: { x: scene.railWidth + 8, y: from.top + markerY },
  });
});

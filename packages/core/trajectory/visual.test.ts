// @vitest-environment node
import { describe, expect, it } from "vitest";
import { emptyTrajectory, sanitizeTrajectoryGraph, type TrajectoryGraph, type TrajectoryNode } from "./graph";
import { buildTrajectoryVisual } from "./visual";

// Folded branches still need separate columns where their intervals overlap.
function fixture(): TrajectoryGraph {
  const graph = emptyTrajectory("A");
  graph.coverage = { complete: true, truncated: false, warnings: [] };
  const issue = (id: string, minute: number, details: TrajectoryNode["details"] = {}): TrajectoryNode => ({
    id: `issue:${id}`, kind: "issue", issue_id: id, title: id, status: "todo",
    created_at: `2026-11-18T01:${String(minute).padStart(2, "0")}:00Z`, details,
  });
  const run = (id: string, owner: string, minute: number, status = "completed", details: TrajectoryNode["details"] = {}): TrajectoryNode => ({
    id: `run:${id}`, kind: "run", issue_id: owner, run_id: id, title: id, status,
    created_at: `2026-11-18T01:${String(minute).padStart(2, "0")}:00Z`, details,
  });
  graph.nodes = [
    issue("A", 0, { run_count: 4 }), issue("B", 2, { parent_id: "A", creation_run_id: "a1", run_count: 2 }),
    issue("C", 4, { parent_id: "B", run_count: 1 }), issue("D", 6, { parent_id: "A", creation_run_id: "b1", run_count: 0 }),
    issue("E", 14, { parent_id: "A", creation_run_id: "a2", run_count: 0 }), issue("Z", 16, { run_count: 0 }),
    run("a1", "A", 1, "completed", { output: "Native text output" }), run("b1", "B", 3, "blocked"),
    run("c1", "C", 5), run("a2", "A", 7), run("a3", "A", 8, "running", { parent_run: "a2" }),
    run("a4", "A", 9), run("b2", "B", 12),
    { id: "artifact:patch", kind: "artifact", issue_id: "A", run_id: "a2", title: "Patch", status: "recorded", created_at: "2026-11-19T01:20:00Z", details: {} },
    { id: "check:test", kind: "check", issue_id: "A", run_id: "a2", title: "Test", status: "passed", created_at: "2026-11-19T01:21:00Z", details: {} },
    { id: "comment:comment", kind: "comment", issue_id: "A", title: "Comment", status: "recorded", created_at: "2026-11-19T01:22:00Z", details: { source_run_id: "a2" } },
    { id: "artifact:foreign", kind: "artifact", issue_id: "B", run_id: "a2", title: "Foreign", status: "recorded", created_at: "2026-11-19T01:23:00Z", details: {} },
  ];
  graph.edges = [{ id: "retry", from: "run:a3", to: "run:a4", kind: "retry" },
    { id: "blocker", from: "run:b1", to: "run:b2", kind: "blocker" }];
  return graph;
}
const matching = () => new Set(["A", "B", "C", "D", "E", "Z"]);
const expanded = () => new Set(["A", "B", "C"]);

describe("trajectory visual scene", () => {
  it("allocates overlapping siblings separately instead of deriving columns from Issue depth", () => {
    const scene = buildTrajectoryVisual(fixture(), matching(), new Set(), new Set());
    expect(scene.rows.map(row => [row.id, row.top, row.height, row.markerX])).toEqual([
      ["issue:A", 0, 44, 8], ["issue:B", 44, 44, 24], ["issue:C", 88, 44, 72],
      ["issue:D", 132, 44, 40], ["issue:E", 176, 44, 56], ["issue:Z", 220, 44, 8],
    ]);
    expect(scene).toMatchObject({ height: 264, railWidth: 92, gutterWidth: 64, gutterCollapseAt: 440, runCount: 0 });
    expect(scene.rows.map(row => row.contentInset)).toEqual([100, 100, 100, 100, 100, 100]);
    expect(scene.rows[0]!.joins.map(join => join.path)).toEqual([
      "M 8 22 C 16.84 22, 24 35.16, 24 44", "M 8 22 C 16.84 22, 40 35.16, 40 44", "M 8 22 C 16.84 22, 56 35.16, 56 44",
    ]);
  });

  it("orders a root's Issues and Runs by time alone in time order, keeping each Run on its owner's lane", () => {
    const task = buildTrajectoryVisual(fixture(), matching(), expanded(), new Set());
    const scene = buildTrajectoryVisual(fixture(), matching(), expanded(), new Set(), "time");
    expect(scene.rows.filter(row => row.kind === "issue" || row.kind === "run").map(row => row.id)).toEqual([
      "issue:A", "run:a1", "issue:B", "run:b1", "issue:C", "run:c1", "issue:D", "run:a2", "run:a3", "run:a4", "run:b2", "issue:E", "issue:Z",
    ]);
    // Scattered among A's Runs, B's Runs still sit on B's branch and keep its colour in both orders.
    const row = (rows: typeof scene.rows, id: string) => rows.find(candidate => candidate.id === id)!;
    expect(row(scene.rows, "run:b1").markerColumn).toBe(row(scene.rows, "issue:B").markerColumn);
    expect(row(scene.rows, "run:b2").colorIndex).toBe(row(scene.rows, "run:b1").colorIndex);
    expect(row(scene.rows, "run:b2").colorIndex).toBe(row(task.rows, "run:b2").colorIndex);
    expect(scene.rows.filter(row => row.kind === "day")).toHaveLength(1);
  });

  it("keeps each Issue's own Runs together right under it, before its sub-Issues, while preserving Run continuations", () => {
    const scene = buildTrajectoryVisual(fixture(), matching(), expanded(), new Set());
    expect(scene.rows.filter(row => row.kind !== "day").map(row => [row.id, row.top, row.height, row.markerY, row.markerX])).toEqual([
      ["issue:A", 48, 44, 22, 8], ["run:a1", 92, 32, 16, 8], ["run:a2", 124, 32, 16, 8],
      ["outputs:run:a2", 156, 24, 12, 72], ["run:a3", 180, 24, 12, 88], ["run:a4", 204, 32, 16, 88],
      ["issue:B", 236, 44, 22, 40], ["run:b1", 280, 24, 12, 40], ["run:b2", 304, 32, 16, 88],
      ["issue:C", 336, 44, 22, 72], ["run:c1", 380, 32, 16, 72], ["issue:D", 412, 44, 22, 24],
      ["issue:E", 456, 44, 22, 56], ["issue:Z", 500, 44, 22, 8],
    ]);
    expect(scene).toMatchObject({ height: 544, railWidth: 108, runCount: 7 });
    const parentRun = scene.rows.find(row => row.id === "run:a2")!;
    expect(parentRun).toMatchObject({ railInset: 43, contentInset: 116, grade: "step", contentHeight: 32, outputCount: 2 });
    expect(parentRun.joins.map(join => [join.laneColumn, join.dash, join.colorIndex])).toEqual([[4, "solid", 4], [5, "dashed", 4], [3, "solid", 0]]);
    // B's blocker continuation b2 and sub-Issue C both end inside B's block, before sibling D.
    const inner = [scene.rows.find(row => row.id === "run:b2")!.markerX, scene.rows.find(row => row.id === "issue:C")!.markerX];
    expect(scene.rows.find(row => row.id === "issue:D")!.segments.some(segment => inner.includes(segment.x))).toBe(false);
  });

  it("folds stored artifact/check ownership into a small count marker and expands only those records", () => {
    const graph = fixture();
    const folded = buildTrajectoryVisual(graph, matching(), expanded(), new Set());
    expect(folded.rows.find(row => row.kind === "outputs")).toMatchObject({ id: "outputs:run:a2", outputCount: 2, markerRadius: 5, height: 24, markerY: 12 });
    expect(folded.rows.some(row => row.id === "outputs:run:a1")).toBe(false);
    const open = buildTrajectoryVisual(graph, matching(), expanded(), new Set(["run:a2"]));
    expect(open.rows.filter(row => row.kind === "record").map(row => [row.id, row.top, row.height, row.markerY, row.markerRadius])).toEqual([
      ["artifact:patch", 156, 28, 14, 10], ["check:test", 184, 28, 14, 10],
    ]);
    expect(open.rows.some(row => row.kind === "outputs")).toBe(false);
    expect(open.height).toBe(576);
    expect(open.rows.some(row => ["comment:comment", "artifact:foreign"].includes(row.id))).toBe(false);
    expect(buildTrajectoryVisual(graph, matching(), expanded(), new Set(["a2"])).rows.filter(row => row.kind === "record")).toHaveLength(2);
  });

  it("anchors Issue creation only to an emitted Run owned by the membership parent", () => {
    const scene = buildTrajectoryVisual(fixture(), matching(), expanded(), new Set());
    expect(scene.rows.find(row => row.id === "run:a1")!.joins).toMatchObject([{ laneId: "issue-branch:B", spineColumn: 0, laneColumn: 2 }]);
    expect(scene.rows.find(row => row.id === "issue:A")!.joins).toMatchObject([{ laneId: "issue-branch:D", spineColumn: 0, laneColumn: 1 }]);
    expect(scene.rows.find(row => row.id === "run:b1")!.joins.every(join => join.laneId !== "issue-branch:D")).toBe(true);
    expect(scene.rows.find(row => row.id === "issue:C")!.colorIndex).toBe(5);
  });

  it("does not turn delegated_from, consumption, or a status label into a structural fork", () => {
    const graph = fixture();
    graph.nodes = graph.nodes.filter(node => node.issue_id === "A" && node.kind !== "artifact" && node.kind !== "check");
    graph.edges = [{ id: "delegation", from: "run:a2", to: "run:a3", kind: "delegation" },
      { id: "consumption", from: "run:a1", to: "run:a4", kind: "consumes" }];
    const nested = graph.nodes.find(node => node.id === "run:a3")!;
    delete nested.details.parent_run;
    nested.details.delegated_from = "a2";
    nested.details.workflow_step = "blocked";
    const scene = buildTrajectoryVisual(graph, new Set(["A"]), new Set(["A"]), new Set());
    expect(scene.runCount).toBe(4);
    expect(scene.rows.every(row => row.markerX === 8)).toBe(true);
    expect(scene.rows.every(row => row.joins.length === 0)).toBe(true);
  });

  it("keeps filtered ancestors as context without rendering unloaded or denied ownership", () => {
    const graph = fixture();
    const filtered = buildTrajectoryVisual(graph, new Set(["B"]), new Set(["A", "B"]), new Set());
    expect(filtered.rows.filter(row => row.kind === "issue").map(row => [row.id, row.context])).toEqual([["issue:A", true], ["issue:B", false], ["issue:C", true]]);
    const denied = sanitizeTrajectoryGraph(graph, new Set(["run:a2", "attachment:patch"]));
    const safe = buildTrajectoryVisual(denied, matching(), expanded(), new Set(["run:a2"]));
    expect(safe.rows.some(row => row.id === "run:a2" || row.id === "artifact:patch" || row.id === "check:test")).toBe(false);
    expect(graph.nodes.find(node => node.id === "run:a2")).toBeDefined();
    expect(safe.rows.find(row => row.id === "issue:A")!.runCount).toBe(4);
  });

  it("uses day rules only when execution is visible, repeats a day after a nested block, and keeps outputs with their Run", () => {
    const graph = fixture();
    const node = graph.nodes.find(node => node.id === "run:b2")!;
    node.created_at = "2026-11-19T01:12:00Z";
    const scene = buildTrajectoryVisual(graph, matching(), expanded(), new Set(["run:a2"]));
    expect(scene.rows.filter(row => row.kind === "day").map(row => [row.height, row.markerY, row.dayLabel])).toEqual([
      [48, null, new Date("2026-11-18T01:00:00Z").toLocaleDateString()],
      [48, null, new Date("2026-11-19T01:12:00Z").toLocaleDateString()],
      [48, null, new Date("2026-11-18T01:00:00Z").toLocaleDateString()],
    ]);
    const ids = scene.rows.map(row => row.kind === "day" ? "day" : row.id);
    expect(ids.slice(ids.indexOf("run:b2") - 1, ids.indexOf("run:b2") + 3)).toEqual(["day", "run:b2", "day", "issue:C"]);
    const index = scene.rows.findIndex(row => row.id === "run:a2");
    expect(scene.rows.slice(index + 1, index + 3).map(row => row.id)).toEqual(["artifact:patch", "check:test"]);
    expect(buildTrajectoryVisual(graph, matching(), new Set(), new Set()).rows.some(row => row.kind === "day")).toBe(false);
  });

  it("requires an explicit Run identity before associating an output with its owner", () => {
    const graph = fixture();
    delete graph.nodes.find(node => node.id === "run:a2")!.run_id;
    delete graph.nodes.find(node => node.id === "artifact:patch")!.run_id;
    const scene = buildTrajectoryVisual(graph, matching(), expanded(), new Set(["run:a2"]));
    expect(scene.rows.find(row => row.id === "run:a2")!.outputCount).toBe(0);
    expect(scene.rows.some(row => row.id === "artifact:patch")).toBe(false);
  });
});

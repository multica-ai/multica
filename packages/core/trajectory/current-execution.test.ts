// @vitest-environment node
import { expect, it } from "vitest";
import type { TrajectoryGraph, TrajectoryNode } from "./graph";
import { deriveCurrentExecution } from "./current-execution";

const issue = (id: string, status: string, details: TrajectoryNode["details"] = {}): TrajectoryNode => ({ id: `issue:${id}`, issue_id: id, kind: "issue", status, title: id, created_at: "2026-11-19T00:00:00Z", details });
const graph = (nodes: TrajectoryNode[]): TrajectoryGraph => ({ root_issue_id: "", nodes, edges: [], findings: [], evidence: [], coverage: { complete: true, truncated: false, warnings: [] } });

it("classifies only matching current Issues into exclusive symbol categories", () => {
  const result = deriveCurrentExecution(graph([
    issue("active", "in_progress"), issue("custom", "custom", { status_category: "started" }),
    issue("blocked", "blocked", { status_category: "done" }), issue("review", "in_review", { status_category: "done" }),
    issue("done", "done", { workflow_step: "blocked", status_category: "started" }),
    issue("failed", "failed", { status_category: "started" }), issue("context", "in_progress"),
  ]), new Set(["active", "custom", "blocked", "review", "done", "failed"]));
  expect(result.total).toBe(6);
  expect(result.unknown).toBe(0);
  expect(result.categories).toEqual({ running: ["active", "custom"], blocked: ["blocked"], review: ["review"] });
  expect([...result.focusIssueIds]).toEqual(["active", "custom", "blocked", "review"]);
});

it("keeps all parallel focuses and their loaded completed ancestors without counting context", () => {
  const result = deriveCurrentExecution(graph([
    issue("root", "done"), issue("parent", "done", { parent_id: "root" }),
    issue("a", "in_progress", { parent_id: "parent" }), issue("b", "blocked", { parent_id: "root" }),
    issue("c", "in_review", { parent_id: "absent" }), issue("unrelated", "in_progress"),
  ]), new Set(["a", "b", "c"]));
  expect(result.categories).toEqual({ running: ["a"], blocked: ["b"], review: ["c"] });
  expect([...result.ancestorIssueIds].sort()).toEqual(["parent", "root"]);
  expect(result.total).toBe(3);
  expect(result.ancestorIssueIds.has("absent")).toBe(false);
});

it("does not let historical blocked Runs or creation evidence turn a done Issue into current blockage", () => {
  const done = issue("done", "done", { creation_source_status: "blocked", creation_run_id: "old", workflow_step: "blocked" });
  const oldRun: TrajectoryNode = { ...issue("done", "completed"), id: "run:old", run_id: "old", kind: "run", details: { workflow_step: "blocked" } };
  const input = graph([done, oldRun, { ...oldRun, id: "run:failed", status: "failed" }, { ...oldRun, id: "attachment:old", kind: "artifact", status: "blocked" }]);
  expect(deriveCurrentExecution(input, new Set(["done"]))).toMatchObject({ total: 1, unknown: 0, categories: { running: [], blocked: [], review: [] } });
  expect(input.nodes).toHaveLength(4);
});

it("deduplicates Issue identity and records missing or unknown window members", () => {
  const result = deriveCurrentExecution(graph([issue("a", "in_progress"), { ...issue("a", "in_progress"), id: "alias:a" }, issue("u", "future")]), new Set(["a", "a", "u", "missing"]));
  expect(result.total).toBe(3);
  expect(result.unknown).toBe(2);
  expect(result.categories).toEqual({ running: ["a"], blocked: [], review: [] });
});

it("keeps pending, cancelled, failed and done members as known history in a no-focus window", () => {
  const input = graph([issue("a", "todo"), issue("b", "backlog"), issue("c", "done"), issue("d", "cancelled"), issue("e", "failed")]);
  const result = deriveCurrentExecution(input, new Set(["a", "b", "c", "d", "e"]));
  expect(result.total).toBe(5);
  expect(result.unknown).toBe(0);
  expect(result.focusIssueIds.size).toBe(0);
  expect(result.ancestorIssueIds.size).toBe(0);
});

it("terminates parent cycles and never reclassifies a parallel focus as ancestor", () => {
  const result = deriveCurrentExecution(graph([issue("a", "blocked", { parent_id: "b" }), issue("b", "in_progress", { parent_id: "a" })]), new Set(["a", "b"]));
  expect(result.categories).toEqual({ running: ["b"], blocked: ["a"], review: [] });
  expect(result.ancestorIssueIds.size).toBe(0);
});

it("returns empty categories for an empty authorized window even if unrelated nodes remain", () => {
  const result = deriveCurrentExecution(graph([issue("old", "blocked")]), new Set());
  expect(result.total).toBe(0);
  expect(result.unknown).toBe(0);
  expect(result.focusIssueIds.size).toBe(0);
});

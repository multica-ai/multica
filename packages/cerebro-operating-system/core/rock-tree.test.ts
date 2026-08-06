import { describe, expect, it } from "vitest";
import { buildRockTree } from "./rock-tree";
import type { RockIssue, RockProject } from "./types";

const project = (id: string, title: string, issue_count = 0, done_issue_count = 0): RockProject => ({ id, title, issue_count, done_issue_count });
const issue = (id: string, over: Partial<RockIssue> = {}): RockIssue => ({
  id, identifier: id.toUpperCase(), title: `Issue ${id}`, status: "todo", ...over,
});

describe("buildRockTree", () => {
  it("returns an empty tree when nothing is connected", () => {
    expect(buildRockTree({ projects: [], issues: [] })).toEqual([]);
  });

  it("hangs a project's issues under the project branch", () => {
    const tree = buildRockTree({
      projects: [project("p1", "Warehouse", 2, 1)],
      issues: [issue("a", { project_id: "p1" }), issue("b", { project_id: "p1", status: "done" })],
    });
    expect(tree).toHaveLength(1);
    expect(tree[0]).toMatchObject({ kind: "project", id: "p1", title: "Warehouse", issueCount: 2, doneIssueCount: 1 });
    expect(tree[0]?.children.map((child) => child.id)).toEqual(["a", "b"]);
  });

  it("nests a sub-issue under its parent inside the same project", () => {
    const tree = buildRockTree({
      projects: [project("p1", "Warehouse")],
      issues: [issue("parent", { project_id: "p1" }), issue("child", { project_id: "p1", parent_id: "parent" })],
    });
    const branch = tree[0]?.children ?? [];
    expect(branch.map((child) => child.id)).toEqual(["parent"]);
    expect(branch[0]?.children.map((child) => child.id)).toEqual(["child"]);
  });

  it("rolls descendant totals up onto the parent issue", () => {
    const tree = buildRockTree({
      projects: [project("p1", "Warehouse")],
      issues: [
        issue("parent", { project_id: "p1" }),
        issue("child", { project_id: "p1", parent_id: "parent", status: "done" }),
        issue("grandchild", { project_id: "p1", parent_id: "child" }),
      ],
    });
    expect(tree[0]?.children[0]).toMatchObject({ id: "parent", issueCount: 2, doneIssueCount: 1 });
  });

  it("places issues without a connected project at the root", () => {
    const tree = buildRockTree({
      projects: [project("p1", "Warehouse")],
      issues: [issue("loose"), issue("other", { project_id: "p-not-connected" })],
    });
    expect(tree.map((node) => node.kind)).toEqual(["project", "issue", "issue"]);
    expect(tree.slice(1).map((node) => node.id)).toEqual(["loose", "other"]);
  });

  it("lifts a sub-issue whose parent lives in another branch", () => {
    const tree = buildRockTree({
      projects: [project("p1", "Warehouse"), project("p2", "Finance")],
      issues: [issue("parent", { project_id: "p1" }), issue("child", { project_id: "p2", parent_id: "parent" })],
    });
    expect(tree[0]?.children.map((child) => child.id)).toEqual(["parent"]);
    expect(tree[1]?.children.map((child) => child.id)).toEqual(["child"]);
  });

  it("lifts a sub-issue whose parent is not connected to the rock at all", () => {
    const tree = buildRockTree({ projects: [], issues: [issue("child", { parent_id: "missing" })] });
    expect(tree.map((node) => node.id)).toEqual(["child"]);
  });

  it("does not recurse forever on a parent cycle", () => {
    const tree = buildRockTree({
      projects: [],
      issues: [issue("a", { parent_id: "b" }), issue("b", { parent_id: "a" })],
    });
    expect(tree.map((node) => node.id).sort()).toEqual(["a", "b"]);
  });

  it("keeps an empty project branch visible", () => {
    const tree = buildRockTree({ projects: [project("p1", "Warehouse")], issues: [] });
    expect(tree[0]).toMatchObject({ kind: "project", children: [] });
  });
});

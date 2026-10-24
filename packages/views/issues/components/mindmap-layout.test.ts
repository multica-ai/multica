// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { Issue, IssueDependency, IssueTableQuerySpec, Project } from "@multica/core/types";
import { buildMindmapLayout, dependencyDirection, mindmapQuery } from "./mindmap-layout";

const project = { id: "project-a", title: "Project A" } as Project;
const issue = (id: string, parent_issue_id: string | null = null): Issue => ({
  id, identifier: id.toUpperCase(), title: id, number: Number(id.slice(-1)) || 1,
  position: 0, project_id: project.id, parent_issue_id,
} as Issue);
const dependency = (issue_id: string, depends_on_issue_id: string, type: IssueDependency["type"]): IssueDependency => ({
  issue_id, depends_on_issue_id, type,
});

describe("mind map layout", () => {
  it("retains children and ancestors when the shared search filters the table", () => {
    const tableQuery: IssueTableQuerySpec = {
      scope: { kind: "workspace" },
      filters: { include_sub_issues: false, statuses: ["todo"] },
      search: "issue-3",
      sort: { field: "position", direction: "asc" },
    };
    expect(mindmapQuery(tableQuery)).toEqual({
      scope: tableQuery.scope,
      filters: { include_sub_issues: true, statuses: ["todo"] },
      sort: tableQuery.sort,
    });
    expect(tableQuery.search).toBe("issue-3");
  });

  it("groups issues under their project and collapses deeper descendants", () => {
    const issues = [issue("issue-1"), issue("issue-2", "issue-1"), issue("issue-3", "issue-2"), issue("issue-4", "issue-3")];
    const collapsed = buildMindmapLayout(issues, [project], [], new Map());
    expect(collapsed.nodes.map((node) => node.key)).toEqual([
      "project:project-a", "issue:issue-1", "issue:issue-2",
    ]);
    expect(collapsed.nodes.find((node) => node.key === "issue:issue-2")?.collapsed).toBe(true);
    const opened = buildMindmapLayout(issues, [project], [], new Map([["issue:issue-2", true], ["issue:issue-3", true]]));
    expect(opened.nodes.map((node) => node.key)).toContain("issue:issue-4");
    expect(opened.hierarchyEdges).toContainEqual({ from: "issue:issue-3", to: "issue:issue-4" });
  });

  it("does not follow cycles or cross-project parents", () => {
    const a = issue("issue-1", "issue-2");
    const b = issue("issue-2", "issue-1");
    const c = { ...issue("issue-3", "issue-1"), project_id: "project-b" };
    const layout = buildMindmapLayout([a, b, c], [project], [], new Map());
    expect(layout.nodes.filter((node) => node.kind === "issue")).toHaveLength(3);
    expect(layout.hierarchyEdges.every((edge) => edge.from.startsWith("project:"))).toBe(true);
  });

  it("normalizes direction, skips related links, and marks collapsed endpoints", () => {
    const issues = [issue("issue-1"), issue("issue-2", "issue-1"), issue("issue-3", "issue-2"), issue("issue-4", "issue-3")];
    const dependencies = [
      dependency("issue-1", "issue-2", "blocks"),
      dependency("issue-2", "issue-1", "blocked_by"),
      dependency("issue-1", "issue-4", "blocks"),
      dependency("issue-2", "issue-3", "related"),
    ];
    const layout = buildMindmapLayout(issues, [project], dependencies, new Map());
    expect(layout.dependencyEdges).toEqual([{ from: "issue:issue-1", to: "issue:issue-2" }]);
    expect(layout.hiddenDependencyCount.get("issue-1")).toBe(1);
    expect(dependencyDirection(dependencies[1]!)).toEqual({ from: "issue-1", to: "issue-2" });
    expect(dependencyDirection(dependencies[3]!)).toBeNull();
  });
});

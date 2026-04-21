import { describe, expect, it } from "vitest";
import type { Project } from "@multica/core/types";
import { sortProjects } from "./sort-projects";

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "project-1",
    workspace_id: "ws-1",
    title: "Alpha",
    description: null,
    icon: null,
    status: "planned",
    priority: "none",
    lead_type: null,
    lead_id: null,
    created_at: "2026-04-10T00:00:00Z",
    updated_at: "2026-04-10T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    ...overrides,
  };
}

describe("sortProjects", () => {
  it("sorts by priority with no-priority treated as lower than low priority", () => {
    const projects = [
      makeProject({ id: "low", title: "Low", priority: "low" }),
      makeProject({ id: "none", title: "No priority", priority: "none" }),
      makeProject({ id: "urgent", title: "Urgent", priority: "urgent" }),
      makeProject({ id: "medium", title: "Medium", priority: "medium" }),
    ];

    expect(sortProjects(projects, "priority", "asc").map((project) => project.id)).toEqual([
      "urgent",
      "medium",
      "low",
      "none",
    ]);
  });

  it("sorts priority descending with no-priority before low priority", () => {
    const projects = [
      makeProject({ id: "low", title: "Low", priority: "low" }),
      makeProject({ id: "none", title: "No priority", priority: "none" }),
      makeProject({ id: "urgent", title: "Urgent", priority: "urgent" }),
      makeProject({ id: "medium", title: "Medium", priority: "medium" }),
    ];

    expect(sortProjects(projects, "priority", "desc").map((project) => project.id)).toEqual([
      "none",
      "low",
      "medium",
      "urgent",
    ]);
  });

  it("keeps original order for items with the same sort value when direction changes", () => {
    const projects = [
      makeProject({ id: "first", priority: "high", created_at: "2026-04-01T00:00:00Z" }),
      makeProject({ id: "second", priority: "high", created_at: "2026-04-20T00:00:00Z" }),
      makeProject({ id: "third", priority: "high", created_at: "2026-04-10T00:00:00Z" }),
    ];

    expect(sortProjects(projects, "priority", "asc").map((project) => project.id)).toEqual([
      "first",
      "second",
      "third",
    ]);
    expect(sortProjects(projects, "priority", "desc").map((project) => project.id)).toEqual([
      "first",
      "second",
      "third",
    ]);
  });

  it("sorts by status using configured status order", () => {
    const projects = [
      makeProject({ id: "completed", status: "completed" }),
      makeProject({ id: "paused", status: "paused" }),
      makeProject({ id: "planned", status: "planned" }),
    ];

    expect(sortProjects(projects, "status", "asc").map((project) => project.id)).toEqual([
      "planned",
      "paused",
      "completed",
    ]);
  });

  it("sorts by title alphabetically", () => {
    const projects = [
      makeProject({ id: "gamma", title: "Gamma" }),
      makeProject({ id: "alpha", title: "Alpha" }),
      makeProject({ id: "beta", title: "Beta" }),
    ];

    expect(sortProjects(projects, "title", "asc").map((project) => project.id)).toEqual([
      "alpha",
      "beta",
      "gamma",
    ]);
  });

  it("sorts by created_at descending", () => {
    const projects = [
      makeProject({ id: "oldest", created_at: "2026-04-01T00:00:00Z" }),
      makeProject({ id: "middle", created_at: "2026-04-10T00:00:00Z" }),
      makeProject({ id: "newest", created_at: "2026-04-20T00:00:00Z" }),
    ];

    expect(sortProjects(projects, "created_at", "desc").map((project) => project.id)).toEqual([
      "newest",
      "middle",
      "oldest",
    ]);
  });

  it("sorts by updated_at descending", () => {
    const projects = [
      makeProject({ id: "oldest", updated_at: "2026-04-01T00:00:00Z" }),
      makeProject({ id: "middle", updated_at: "2026-04-10T00:00:00Z" }),
      makeProject({ id: "newest", updated_at: "2026-04-20T00:00:00Z" }),
    ];

    expect(sortProjects(projects, "updated_at", "desc").map((project) => project.id)).toEqual([
      "newest",
      "middle",
      "oldest",
    ]);
  });
});

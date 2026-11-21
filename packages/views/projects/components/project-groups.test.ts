import { describe, expect, it } from "vitest";
import type { Project } from "@multica/core/types";
import { NO_FOLDER_KEY, NO_LEAD_KEY, groupProjects, projectGroupKey, projectGroupUpdate } from "./project-groups";

const base: Project = {
  id: "p1",
  workspace_id: "ws",
  title: "One",
  description: null,
  icon: null,
  status: "planned",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};

describe("project groups", () => {
  it("keys a project by the grouped field", () => {
    const led = { ...base, lead_type: "agent" as const, lead_id: "a1" };
    expect(projectGroupKey(led, "status")).toBe("planned");
    expect(projectGroupKey(led, "priority")).toBe("none");
    expect(projectGroupKey(led, "lead")).toBe("agent:a1");
    expect(projectGroupKey(base, "lead")).toBe(NO_LEAD_KEY);
  });

  it("turns a drop target into the update that moves a project there", () => {
    expect(projectGroupUpdate("status", "paused")).toEqual({ status: "paused" });
    expect(projectGroupUpdate("priority", "urgent")).toEqual({ priority: "urgent" });
    expect(projectGroupUpdate("lead", "member:u1")).toEqual({ lead_type: "member", lead_id: "u1" });
    expect(projectGroupUpdate("lead", NO_LEAD_KEY)).toEqual({ lead_type: null, lead_id: null });
  });

  it("puts a project whose folder is gone with the projects outside every folder", () => {
    const projects = [
      { ...base, id: "a" },
      { ...base, id: "b" },
      { ...base, id: "c" },
    ];
    const folderOf = new Map([
      ["a", "f1"],
      ["c", "deleted"],
    ]);
    expect(projectGroupKey(projects[1]!, "folder", folderOf)).toBe(NO_FOLDER_KEY);
    const buckets = groupProjects(projects, "folder", ["f1", NO_FOLDER_KEY], folderOf);
    expect(buckets.map((b) => [b.key, b.projects.map((p) => p.id)])).toEqual([
      ["f1", ["a"]],
      [NO_FOLDER_KEY, ["b", "c"]],
    ]);
  });

  it("buckets in key order, keeps row order and empty buckets, drops unlisted keys", () => {
    const projects = [
      { ...base, id: "a", status: "in_progress" as const },
      { ...base, id: "b", status: "planned" as const },
      { ...base, id: "c", status: "in_progress" as const },
      { ...base, id: "d", status: "cancelled" as const },
    ];
    const buckets = groupProjects(projects, "status", ["planned", "in_progress", "paused"]);
    expect(buckets.map((b) => [b.key, b.projects.map((p) => p.id)])).toEqual([
      ["planned", ["b"]],
      ["in_progress", ["a", "c"]],
      ["paused", []],
    ]);
  });
});

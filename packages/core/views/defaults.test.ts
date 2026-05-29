import { describe, it, expect } from "vitest";
import { DEFAULT_VIEWS, buildDefaultViewRequests } from "./defaults";

describe("buildDefaultViewRequests", () => {
  const upper = (k: string) => k.toUpperCase();

  it("seeds issues defaults as shared views in order, resolving names", () => {
    const reqs = buildDefaultViewRequests("issues", null, upper);
    expect(reqs.map((r) => r.name)).toEqual(["ALL", "MEMBERS", "AGENTS"]);
    expect(reqs.every((r) => r.shared === true)).toBe(true);
    expect(reqs.map((r) => r.position)).toEqual([0, 1, 2]);
    expect(reqs[0]!.filters).toEqual({});
    expect(reqs[1]!.filters).toEqual({ assignee_types: ["member"] });
  });

  it("threads projectId through for the project page page-scope", () => {
    const reqs = buildDefaultViewRequests("issues", "proj-1", upper);
    expect(reqs.every((r) => r.project_id === "proj-1")).toBe(true);
  });

  it("seeds my_issues All as an any_of view across assignee/creator/agents", () => {
    const reqs = buildDefaultViewRequests("my_issues", null, upper);
    expect(reqs.map((r) => r.name)).toEqual(["ALL", "ASSIGNED", "CREATED", "AGENTS"]);
    expect(reqs[0]!.filters?.any_of).toHaveLength(3);
  });

  it("first default of each page is the active-by-default tab", () => {
    expect(DEFAULT_VIEWS.issues[0]!.nameKey).toBe("all");
    expect(DEFAULT_VIEWS.my_issues[0]!.nameKey).toBe("all");
  });
});

// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const project = {
  id: "project", workspace_id: "workspace", title: "Project", description: null,
  icon: null, status: "planned", priority: "none", lead_type: null, lead_id: null,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

const endpoints = ["list", "get", "create", "update"] as const;
afterEach(() => vi.unstubAllGlobals());

describe("project API response boundaries", () => {
  it.each(endpoints)("validates %s responses from older backends", async (endpoint) => {
    const body = endpoint === "list" ? { projects: [project], total: 1 } : project;
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async () =>
      new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }),
    ));
    const client = new ApiClient("https://api.example.test");
    const result = endpoint === "list" ? (await client.listProjects()).projects[0] :
      endpoint === "get" ? await client.getProject("project") :
      endpoint === "create" ? await client.createProject({ title: "Project" }) :
      await client.updateProject("project", { title: "Project" });
    expect(result).toMatchObject({ id: "project", creator_type: null, creator_id: null });
  });

  it.each(endpoints)("falls back on malformed %s responses", async (endpoint) => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async () =>
      new Response(JSON.stringify({ id: 42, projects: "invalid" }), { headers: { "Content-Type": "application/json" } }),
    ));
    const client = new ApiClient("https://api.example.test");
    if (endpoint === "list") {
      expect(await client.listProjects()).toEqual({ projects: [], total: 0 });
    } else {
      const result = endpoint === "get" ? await client.getProject("project") :
        endpoint === "create" ? await client.createProject({ title: "Project" }) :
        await client.updateProject("project", { title: "Project" });
      expect(result).toMatchObject({ id: "", creator_type: null, creator_id: null });
    }
  });
});

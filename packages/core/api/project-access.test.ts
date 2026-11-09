// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { configStore } from "../config";
import { ProjectSchema, EMPTY_PROJECT } from "./schemas";

afterEach(() => { vi.unstubAllGlobals(); configStore.getState().setFeatureFlags({}); });

describe("project access API contracts", () => {
  it("keeps old projects readable and treats unknown visibility as restricted", () => {
    expect(ProjectSchema.parse(EMPTY_PROJECT).visibility).toBe("workspace");
    expect(ProjectSchema.parse({ ...EMPTY_PROJECT, visibility: "future-scope" }).visibility).toBe("private");
    expect(ProjectSchema.parse({ ...EMPTY_PROJECT, visibility: null }).visibility).toBe("private");
  });

  it("blocks private creation on an older server before it can drop access fields", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    await expect(client.createProject({ title: "Secret", visibility: "private", member_ids: ["member-1"] })).rejects.toThrow(/not enabled/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("sends private creation and member changes with their project-scoped contracts", async () => {
    configStore.getState().setFeatureFlags({ private_projects_v1: true });
    const member = { project_id: "p1", user_id: "m1", name: "Alice", email: "alice@example.test" };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...EMPTY_PROJECT, id: "p1", visibility: "private" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: [member], total: 1 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: "invalid", total: 1 })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const created = await client.createProject({ title: "Secret", visibility: "private", member_ids: ["m1"] });
    expect(created.visibility).toBe("private");
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1].body)).toMatchObject({ visibility: "private", member_ids: ["m1"] });
    const added = await client.addProjectMembers("p1", ["m1"]);
    expect(added.members[0]).toEqual({ ...member, avatar_url: null });
    expect(fetchMock.mock.calls[1]?.[0]).toBe("https://api.example.test/api/projects/p1/members");
    expect(JSON.parse(fetchMock.mock.calls[1]?.[1].body)).toEqual({ user_ids: ["m1"] });
    expect(await client.getProjectMembers("p1")).toEqual({ members: [], total: 0 });
    await client.removeProjectMember("p1", "m1");
    expect(fetchMock.mock.calls[3]?.[0]).toBe("https://api.example.test/api/projects/p1/members/m1");
    expect(fetchMock.mock.calls[3]?.[1].method).toBe("DELETE");
  });
});

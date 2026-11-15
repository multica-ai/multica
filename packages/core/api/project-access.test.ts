// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { ProjectSchema, EMPTY_PROJECT } from "./schemas";

afterEach(() => { vi.unstubAllGlobals(); });

describe("project access API contracts", () => {
  it("keeps old projects readable and treats unknown visibility as restricted", () => {
    expect(ProjectSchema.parse(EMPTY_PROJECT).visibility).toBe("workspace");
    expect(ProjectSchema.parse({ ...EMPTY_PROJECT, visibility: "future-scope" }).visibility).toBe("private");
    expect(ProjectSchema.parse({ ...EMPTY_PROJECT, visibility: null }).visibility).toBe("private");
  });

  it("sends private creation and member changes with their project-scoped contracts", async () => {
    const member = { project_id: "p1", user_id: "m1", name: "Alice", email: "alice@example.test" };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...EMPTY_PROJECT, id: "p1", visibility: "private" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: [member], total: 1 })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: "invalid", total: 1 })))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: [member], total: 1, manager_id: "m1" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ members: [member], total: 1, manager_id: 7 })));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const created = await client.createProject({ title: "Secret", visibility: "private", member_ids: ["m1"] });
    expect(created.visibility).toBe("private");
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1].body)).toMatchObject({ visibility: "private", member_ids: ["m1"] });
    const added = await client.addProjectMembers("p1", ["m1"]);
    expect(added.members[0]).toEqual({ ...member, avatar_url: null });
    expect(fetchMock.mock.calls[1]?.[0]).toBe("https://api.example.test/api/projects/p1/members");
    expect(JSON.parse(fetchMock.mock.calls[1]?.[1].body)).toEqual({ user_ids: ["m1"] });
    // A server that predates manager_id names nobody.
    expect(added.manager_id).toBeNull();
    expect(await client.getProjectMembers("p1")).toEqual({ members: [], total: 0, manager_id: null });
    await client.removeProjectMember("p1", "m1");
    expect(fetchMock.mock.calls[3]?.[0]).toBe("https://api.example.test/api/projects/p1/members/m1");
    expect(fetchMock.mock.calls[3]?.[1].method).toBe("DELETE");
    expect((await client.getProjectMembers("p1")).manager_id).toBe("m1");
    // A malformed manager costs the answer, not the member list.
    expect(await client.getProjectMembers("p1")).toMatchObject({ members: [{ user_id: "m1" }], manager_id: null });
  });

  it("reads the owner's overview of private projects and survives malformed rows", async () => {
    const member = { user_id: "m1", name: "Alice", email: "alice@example.test" };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        projects: [
          { id: "p1", title: "Secret", lead_type: "member", lead_id: "m1", created_at: "2026-01-01T00:00:00Z", is_member: false, members: [member] },
          // No id: dropped. One bad row must not hide the others from the
          // only person who can still reach them.
          { title: "Broken" },
          // Wrong field shapes degrade to safe values instead of failing.
          { id: "p2", title: 7, lead_type: "robot", is_member: "yes", members: "invalid" },
        ],
        total: 3,
      })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ projects: "invalid" })))
      .mockResolvedValueOnce(new Response(JSON.stringify(null)))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ...EMPTY_PROJECT, id: "p1", visibility: "private" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ id: 1 })));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    const overview = await client.listPrivateProjectsForOwner();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/projects/private");
    expect(overview.projects).toEqual([
      { id: "p1", title: "Secret", icon: null, lead_type: "member", lead_id: "m1", created_at: "2026-01-01T00:00:00Z", archived_at: null, is_member: false, members: [{ ...member, avatar_url: null }] },
      { id: "p2", title: "", icon: null, lead_type: null, lead_id: null, created_at: "", archived_at: null, is_member: false, members: [] },
    ]);
    expect((await client.listPrivateProjectsForOwner()).projects).toEqual([]);
    expect(await client.listPrivateProjectsForOwner()).toEqual({ projects: [], total: 0 });

    const joined = await client.joinPrivateProject("p1");
    expect(fetchMock.mock.calls[3]?.[0]).toBe("https://api.example.test/api/projects/p1/join");
    expect(fetchMock.mock.calls[3]?.[1].method).toBe("POST");
    expect(joined).toMatchObject({ id: "p1", visibility: "private" });
    expect(await client.joinPrivateProject("p1")).toEqual(EMPTY_PROJECT);
  });
});

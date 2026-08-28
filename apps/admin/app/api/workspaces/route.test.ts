import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

vi.mock("@/lib/queries", () => ({ listWorkspaces: vi.fn() }));
vi.mock("@/lib/litellm-join", () => ({ attachLiteLlmToList: vi.fn() }));

const { listWorkspaces } = await import("@/lib/queries");
const { attachLiteLlmToList } = await import("@/lib/litellm-join");
const { GET } = await import("./route");

describe("GET /api/workspaces?all=true", () => {
  beforeEach(() => vi.clearAllMocks());

  it("exports the full roster without inferring a team mapping", async () => {
    const dbItems = [
      { id: "1", name: "Alpha", slug: "alpha", owner: null, model: null, llmKey: null, team: null, keySpend: null, status: "idle", openIssues: 0, lastActivity: null },
      { id: "2", name: "Beta", slug: "beta", owner: null, model: null, llmKey: null, team: null, keySpend: null, status: "active", openIssues: 1, lastActivity: "2026-08-28T10:00:00.000Z" },
    ] as const;
    vi.mocked(listWorkspaces).mockResolvedValue({ items: [...dbItems], total: 2, page: 1, pageSize: 50 });
    const response = await GET(new NextRequest("http://localhost/api/workspaces?all=true"));

    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({
      total: 2,
      items: [
        { id: "1", name: "Alpha", slug: "alpha", status: "idle", openIssues: 0, lastActivity: null },
        { id: "2", name: "Beta", slug: "beta", status: "active", openIssues: 1, lastActivity: "2026-08-28T10:00:00.000Z" },
      ],
    });
    expect(listWorkspaces).toHaveBeenCalledWith(expect.any(Object), { unpaged: true });
    expect(attachLiteLlmToList).not.toHaveBeenCalled();
  });

  it("does not silently return a partial export", async () => {
    vi.mocked(listWorkspaces).mockResolvedValue({ items: [], total: 5001, page: 1, pageSize: 50 });

    const response = await GET(new NextRequest("http://localhost/api/workspaces?all=true"));

    expect(response.status).toBe(413);
    expect(attachLiteLlmToList).not.toHaveBeenCalled();
  });
});

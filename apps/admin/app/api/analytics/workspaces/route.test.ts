import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

vi.mock("@/lib/queries", () => ({
  getAnalyticsWorkspaceBreakdown: vi.fn(),
}));

const { getAnalyticsWorkspaceBreakdown } = await import("@/lib/queries");
const { GET } = await import("./route");

function request(search: string): NextRequest {
  return new NextRequest(`http://localhost/api/analytics/workspaces?${search}`);
}

const TIME = "from=2026-08-20T00%3A00%3A00.000Z&to=2026-08-20T01%3A00%3A00.000Z";

describe("GET /api/analytics/workspaces", () => {
  beforeEach(() => vi.clearAllMocks());

  it("folds raw error reasons into the selected class and totals each workspace", async () => {
    vi.mocked(getAnalyticsWorkspaceBreakdown).mockResolvedValue([
      { workspaceId: "a", workspaceName: "Acme", segment: "agent_error.provider_auth_or_access", count: 2 },
      { workspaceId: "a", workspaceName: "Acme", segment: "agent_error.missing_config", count: 1 },
      { workspaceId: "b", workspaceName: "Beta", segment: "agent_error.provider_auth_or_access", count: 4 },
      { workspaceId: "b", workspaceName: "Beta", segment: "timeout", count: 9 },
    ]);

    const res = await GET(request(`${TIME}&kind=errors&segment=auth`));

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      items: [
        { workspaceId: "b", workspaceName: "Beta", count: 4 },
        { workspaceId: "a", workspaceName: "Acme", count: 3 },
      ],
    });
    expect(getAnalyticsWorkspaceBreakdown).toHaveBeenCalledWith({
      from: "2026-08-20T00:00:00.000Z",
      to: "2026-08-20T01:00:00.000Z",
      kind: "errors",
    });
  });

  it("groups active autopilot statuses into the in-flight segment", async () => {
    vi.mocked(getAnalyticsWorkspaceBreakdown).mockResolvedValue([
      { workspaceId: "a", workspaceName: "Acme", segment: "running", count: 2 },
      { workspaceId: "a", workspaceName: "Acme", segment: "issue_created", count: 1 },
      { workspaceId: "b", workspaceName: "Beta", segment: "completed", count: 3 },
    ]);

    const res = await GET(request(`${TIME}&kind=autopilotRuns&segment=other`));

    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      items: [{ workspaceId: "a", workspaceName: "Acme", count: 3 }],
    });
  });

  it("rejects unknown segments before querying", async () => {
    const res = await GET(request(`${TIME}&kind=errors&segment=not-a-class`));

    expect(res.status).toBe(400);
    expect(getAnalyticsWorkspaceBreakdown).not.toHaveBeenCalled();
  });

  it("rejects a breakdown range wider than the largest available bucket", async () => {
    const res = await GET(request("from=2026-08-01T00%3A00%3A00.000Z&to=2026-08-09T00%3A00%3A00.001Z&kind=errors&segment=auth"));

    expect(res.status).toBe(400);
    expect(getAnalyticsWorkspaceBreakdown).not.toHaveBeenCalled();
  });
});

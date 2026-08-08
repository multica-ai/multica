import { beforeEach, describe, expect, it, vi } from "vitest";

const { getCerebroDashboardOverview, getCerebroDashboardAllMessages } = vi.hoisted(() => ({
  getCerebroDashboardOverview: vi.fn(async () => ({})),
  getCerebroDashboardAllMessages: vi.fn(async () => ({})),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { getCerebroDashboardOverview, getCerebroDashboardAllMessages },
}));

import { fetchAllMessages, fetchDashboardOverview } from "./api";
import { dashboardKeys } from "./queries";

const bounds = { start: "2026-08-03T00:00:00.000Z", end: "2026-08-04T00:00:00.000Z" };

describe("dashboard exact time bounds", () => {
  beforeEach(() => {
    getCerebroDashboardOverview.mockClear();
    getCerebroDashboardAllMessages.mockClear();
  });

  it("forwards exact bounds to the overview endpoint", async () => {
    await fetchDashboardOverview("30d", "all", null, bounds);
    expect(getCerebroDashboardOverview).toHaveBeenCalledWith("30d", {
      actor_type: undefined,
      actor_id: null,
      start: bounds.start,
      end: bounds.end,
    });
  });

  it("forwards exact bounds to the all-messages endpoint", async () => {
    await fetchAllMessages("30d", "agents", "agent-1", bounds);
    expect(getCerebroDashboardAllMessages).toHaveBeenCalledWith("30d", {
      actor_type: "agent",
      actor_id: "agent-1",
      start: bounds.start,
      end: bounds.end,
    });
  });

  it("omits bounds when no exact range is selected", async () => {
    await fetchDashboardOverview("7d", "all", null, null);
    expect(getCerebroDashboardOverview).toHaveBeenCalledWith("7d", {
      actor_type: undefined,
      actor_id: null,
      start: null,
      end: null,
    });
  });

  it("keys the overview and all-messages caches on the exact bounds", () => {
    const withBounds = dashboardKeys.overview("ws", "30d", "all", null, bounds);
    const withoutBounds = dashboardKeys.overview("ws", "30d", "all", null, null);
    expect(withBounds).not.toEqual(withoutBounds);
    expect(withBounds).toContain(bounds.start);
    expect(withBounds).toContain(bounds.end);
    expect(dashboardKeys.allMessages("ws", "30d", "all", null, bounds)).toContain(bounds.start);
  });
});

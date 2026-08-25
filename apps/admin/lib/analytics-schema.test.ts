import { describe, expect, it } from "vitest";
import { parseAnalyticsWorkspaceBreakdown } from "./analytics-schema";

describe("parseAnalyticsWorkspaceBreakdown", () => {
  it("returns workspace rows from a valid response", () => {
    expect(parseAnalyticsWorkspaceBreakdown({
      items: [{ workspaceId: "ws-1", workspaceName: "Acme", count: 3 }],
    })).toEqual({
      items: [{ workspaceId: "ws-1", workspaceName: "Acme", count: 3 }],
    });
  });

  it("falls back to an empty breakdown when items is missing or malformed", () => {
    expect(parseAnalyticsWorkspaceBreakdown({ items: null })).toEqual({ items: [] });
    expect(parseAnalyticsWorkspaceBreakdown({})).toEqual({ items: [] });
  });
});

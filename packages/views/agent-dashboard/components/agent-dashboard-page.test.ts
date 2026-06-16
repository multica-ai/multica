import { describe, expect, it } from "vitest";
import {
  parseDashboardOwnerSelection,
  parseHour,
} from "./agent-dashboard-page";

describe("parseHour", () => {
  it("uses the fallback when the hour param is missing or empty", () => {
    expect(parseHour(null, 23)).toBe(23);
    expect(parseHour("", 23)).toBe(23);
    expect(parseHour("   ", 23)).toBe(23);
  });

  it("parses valid hour params", () => {
    expect(parseHour("0", 23)).toBe(0);
    expect(parseHour("23", 0)).toBe(23);
  });

  it("uses the fallback for invalid hour params", () => {
    expect(parseHour("24", 23)).toBe(23);
    expect(parseHour("-1", 0)).toBe(0);
    expect(parseHour("nope", 23)).toBe(23);
  });
});

describe("parseDashboardOwnerSelection", () => {
  it("defaults to the current user when the URL has no owner parameter", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams(), "user-1")).toEqual({
      selectedOwnerId: "user-1",
      followsCurrentUserDefault: true,
    });
  });

  it("treats stale all-members URLs as the current-user default", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams("owner=all"), "user-1")).toEqual({
      selectedOwnerId: "user-1",
      followsCurrentUserDefault: true,
    });
  });

  it("ignores explicit owner params and keeps the current-user default", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams("owner=user-2"), "user-1")).toEqual({
      selectedOwnerId: "user-1",
      followsCurrentUserDefault: true,
    });
  });
});

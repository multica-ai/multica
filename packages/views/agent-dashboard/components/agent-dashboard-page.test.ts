import { describe, expect, it } from "vitest";
import {
  ownerFilterDisplayLabel,
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

describe("ownerFilterDisplayLabel", () => {
  it("shows the selected member name instead of the user id", () => {
    expect(
      ownerFilterDisplayLabel({
        selectedOwnerId: "user-2",
        selectedMember: { name: "Alice Zhang", email: "alice@example.com" },
        allOwnersLabel: "All members",
        selectedOwnerFallback: "Selected member",
      }),
    ).toBe("Alice Zhang");
  });

  it("falls back without exposing raw ids", () => {
    expect(
      ownerFilterDisplayLabel({
        selectedOwnerId: "user-2",
        selectedMember: null,
        allOwnersLabel: "All members",
        selectedOwnerFallback: "Selected member",
      }),
    ).toBe("Selected member");
  });
});

describe("parseDashboardOwnerSelection", () => {
  it("defaults to the current user when the URL has no owner parameter", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams(), "user-1")).toEqual({
      selectedOwnerId: "user-1",
      explicitAll: false,
      followsCurrentUserDefault: true,
    });
  });

  it("keeps an explicit all-members selection unowned", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams("owner=all"), "user-1")).toEqual({
      selectedOwnerId: null,
      explicitAll: true,
      followsCurrentUserDefault: false,
    });
  });

  it("uses an explicit owner instead of the current-user default", () => {
    expect(parseDashboardOwnerSelection(new URLSearchParams("owner=user-2"), "user-1")).toEqual({
      selectedOwnerId: "user-2",
      explicitAll: false,
      followsCurrentUserDefault: false,
    });
  });
});

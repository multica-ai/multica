import { describe, expect, it } from "vitest";
import { ownerFilterDisplayLabel } from "./agent-dashboard-page";

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

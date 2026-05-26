import { describe, expect, it } from "vitest";
import { ALL_STATUSES, BOARD_STATUSES, STATUS_CONFIG, STATUS_ORDER } from "./status";

describe("issue status config", () => {
  it("includes polling across the shared status lists", () => {
    expect(STATUS_ORDER).toContain("polling");
    expect(ALL_STATUSES).toContain("polling");
    expect(BOARD_STATUSES).toContain("polling");
    expect(STATUS_CONFIG.polling.label).toBe("Polling");
  });
});

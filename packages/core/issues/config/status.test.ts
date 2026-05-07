import { describe, expect, it } from "vitest";
import { ALL_STATUSES, BOARD_STATUSES, STATUS_CONFIG, STATUS_ORDER } from "./status";

describe("issue status config", () => {
  it("includes archive as a selectable issue status", () => {
    expect(STATUS_ORDER).toContain("archive");
    expect(ALL_STATUSES).toContain("archive");
    expect(STATUS_CONFIG.archive.label).toBe("Archive");
  });

  it("excludes archive from board columns", () => {
    expect(BOARD_STATUSES).not.toContain("archive");
  });
});

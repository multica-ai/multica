import { describe, expect, it } from "vitest";

import {
  CLOSED_STATUSES,
  COMPLETED_STATUSES,
  isClosedStatus,
  isCompletedStatus,
} from "./status";

// Mirrors server/internal/issueguard/issue_status_test.go and the Postgres
// functions in migration 235 — all three encode the closed-vs-completed split.
describe("issue status classification", () => {
  it("closed = done | cancelled | archived", () => {
    expect(CLOSED_STATUSES).toEqual(["done", "cancelled", "archived"]);
    expect(isClosedStatus("done")).toBe(true);
    expect(isClosedStatus("cancelled")).toBe(true);
    expect(isClosedStatus("archived")).toBe(true);
    expect(isClosedStatus("in_progress")).toBe(false);
    expect(isClosedStatus("backlog")).toBe(false);
  });

  it("completed = done | cancelled (archived excluded)", () => {
    expect(COMPLETED_STATUSES).toEqual(["done", "cancelled"]);
    expect(isCompletedStatus("done")).toBe(true);
    expect(isCompletedStatus("cancelled")).toBe(true);
    expect(isCompletedStatus("archived")).toBe(false);
    expect(isCompletedStatus("in_progress")).toBe(false);
  });

  it("archived is closed but not completed", () => {
    expect(isClosedStatus("archived")).toBe(true);
    expect(isCompletedStatus("archived")).toBe(false);
  });

  it("completed is a strict subset of closed", () => {
    for (const s of COMPLETED_STATUSES) {
      expect(isClosedStatus(s)).toBe(true);
    }
  });
});

import { describe, expect, it } from "vitest";
import { crAttemptListOptions } from "./cr-attempts";

describe("crAttemptListOptions", () => {
  it("polls while the issue is open in CodeRabbit", () => {
    expect(crAttemptListOptions("ws-1", "issue-1", true).refetchInterval).toBe(15_000);
  });

  it("does not poll when the issue is not open in CodeRabbit", () => {
    expect(crAttemptListOptions("ws-1", "issue-1", false).refetchInterval).toBe(false);
  });
});

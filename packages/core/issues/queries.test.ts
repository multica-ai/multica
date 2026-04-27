import { describe, expect, it } from "vitest";
import { issueDetailOptions, issueTimelineOptions } from "./queries";

describe("issueTimelineOptions", () => {
  it("includes a foreground-only polling interval as a WS safety net", () => {
    const opts = issueTimelineOptions("issue-1");
    // Polling interval is set (exact ms can evolve without breaking the test).
    expect(typeof opts.refetchInterval).toBe("number");
    expect(opts.refetchInterval as number).toBeGreaterThan(0);
    // Must not poll when the tab is backgrounded — otherwise idle tabs hammer the API.
    expect(opts.refetchIntervalInBackground).toBe(false);
  });
});

describe("issueDetailOptions", () => {
  it("includes a foreground-only polling interval as a WS safety net", () => {
    const opts = issueDetailOptions("ws-1", "issue-1");
    expect(typeof opts.refetchInterval).toBe("number");
    expect(opts.refetchInterval as number).toBeGreaterThan(0);
    expect(opts.refetchIntervalInBackground).toBe(false);
  });

  it("polls detail slower than timeline since status changes are rarer than comments", () => {
    const detail = issueDetailOptions("ws-1", "issue-1").refetchInterval as number;
    const timeline = issueTimelineOptions("issue-1").refetchInterval as number;
    expect(detail).toBeGreaterThan(timeline);
  });
});

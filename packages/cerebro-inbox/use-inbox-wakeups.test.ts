import { describe, expect, it } from "vitest";
import { buildInboxWakeupHints } from "./use-inbox-wakeups";

describe("buildInboxWakeupHints", () => {
  it("returns an empty map for no wakeups", () => {
    expect(buildInboxWakeupHints([]).size).toBe(0);
  });

  it("maps one wakeup per issue with a fireAt for time triggers", () => {
    const hints = buildInboxWakeupHints([
      { issue_id: "i-1", trigger_type: "time", fire_at: "2026-06-10T14:30:00Z" },
    ]);
    expect(hints.get("i-1")?.fireAt).toBe("2026-06-10T14:30:00Z");
    expect(hints.get("i-1")?.title).toContain("Planlagt");
  });

  it("keeps the earliest time trigger when an issue has several", () => {
    const hints = buildInboxWakeupHints([
      { issue_id: "i-1", trigger_type: "time", fire_at: "2026-06-10T16:00:00Z" },
      { issue_id: "i-1", trigger_type: "time", fire_at: "2026-06-10T09:00:00Z" },
    ]);
    expect(hints.get("i-1")?.fireAt).toBe("2026-06-10T09:00:00Z");
  });

  it("prefers a concrete time trigger over a condition trigger", () => {
    const hints = buildInboxWakeupHints([
      { issue_id: "i-1", trigger_type: "issue_status" },
      { issue_id: "i-1", trigger_type: "time", fire_at: "2026-06-10T09:00:00Z" },
    ]);
    expect(hints.get("i-1")?.fireAt).toBe("2026-06-10T09:00:00Z");
  });

  it("falls back to a condition-trigger hint with no fireAt", () => {
    const hints = buildInboxWakeupHints([
      { issue_id: "i-1", trigger_type: "github_ci" },
    ]);
    expect(hints.get("i-1")?.fireAt).toBeUndefined();
    expect(hints.get("i-1")?.title).toContain("CI");
  });

  it("ignores wakeups with no issue_id", () => {
    expect(buildInboxWakeupHints([{ issue_id: "", trigger_type: "time", fire_at: "x" }]).size).toBe(0);
  });
});

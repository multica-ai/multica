import { describe, expect, it } from "vitest";
import {
  parseRuntimePauseWaitReason,
  runtimePauseQueuedLabel,
} from "./runtime-pause-wait-reason";

describe("parseRuntimePauseWaitReason", () => {
  it("parses stamped wait_reason", () => {
    const parsed = parseRuntimePauseWaitReason(
      "runtime_paused|rate_limit|2026-06-01T11:55:00Z",
    );
    expect(parsed).toEqual({
      pauseReason: "rate_limit",
      unpauseAt: "2026-06-01T11:55:00Z",
    });
  });

  it("returns null for unrelated wait_reason", () => {
    expect(parseRuntimePauseWaitReason("/path/lock")).toBeNull();
  });
});

describe("runtimePauseQueuedLabel", () => {
  it("includes retry time when scheduled", () => {
    const label = runtimePauseQueuedLabel({
      pauseReason: "rate_limit",
      unpauseAt: "2026-06-01T11:55:00Z",
    });
    expect(label).toContain("rate limit");
    expect(label).toContain("retry");
  });

  it("omits retry when circuit is open", () => {
    const label = runtimePauseQueuedLabel({
      pauseReason: "rate_limit",
      unpauseAt: null,
    });
    expect(label).toBe("runtime paused (rate limit or usage limit)");
    expect(label).not.toContain("retry");
  });
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineItem } from "./build-timeline";
import {
  contextUsagePercent,
  isContextUsageItem,
  latestContextUsage,
  readContextEvent,
} from "./context-usage";

function contextItem(seq: number, input: Record<string, unknown>): TimelineItem {
  return { seq, type: "context", input, content: "Context 1/2" };
}

describe("readContextEvent", () => {
  it("reads an occupancy observation", () => {
    expect(
      readContextEvent(
        contextItem(1, { used_tokens: 112_400, limit_tokens: 200_000, model: "claude-opus-5" }),
      ),
    ).toEqual({
      kind: "usage",
      usedTokens: 112_400,
      limitTokens: 200_000,
      model: "claude-opus-5",
    });
  });

  it("leaves an unreported window undefined rather than zero", () => {
    expect(readContextEvent(contextItem(1, { used_tokens: 500 }))).toEqual({
      kind: "usage",
      usedTokens: 500,
      limitTokens: undefined,
      model: undefined,
    });
  });

  it("reads a compaction even when its metadata is missing", () => {
    expect(readContextEvent(contextItem(1, { phase: "compacted" }))).toEqual({
      kind: "compaction",
      preTokens: undefined,
      trigger: undefined,
    });
    expect(
      readContextEvent(contextItem(1, { phase: "compacted", pre_tokens: 184_000, trigger: "auto" })),
    ).toEqual({ kind: "compaction", preTokens: 184_000, trigger: "auto" });
  });

  it("ignores other event types and payloads with no usable figure", () => {
    expect(readContextEvent({ seq: 1, type: "text", content: "hi" })).toBeNull();
    expect(readContextEvent(contextItem(1, {}))).toBeNull();
    expect(readContextEvent(contextItem(1, { used_tokens: "many" }))).toBeNull();
  });
});

describe("latestContextUsage", () => {
  it("takes the newest observation, not the sum of them", () => {
    const items: TimelineItem[] = [
      contextItem(1, { used_tokens: 10_000, limit_tokens: 200_000 }),
      { seq: 2, type: "text", content: "working" },
      contextItem(3, { used_tokens: 90_000, limit_tokens: 200_000 }),
    ];
    expect(latestContextUsage(items)).toEqual({
      usedTokens: 90_000,
      limitTokens: 200_000,
      model: undefined,
    });
  });

  it("skips a trailing compaction, which carries no occupancy of its own", () => {
    const items: TimelineItem[] = [
      contextItem(1, { used_tokens: 180_000, limit_tokens: 200_000 }),
      contextItem(2, { phase: "compacted", pre_tokens: 180_000 }),
    ];
    expect(latestContextUsage(items)?.usedTokens).toBe(180_000);
  });

  it("is undefined when the runtime reported nothing", () => {
    expect(latestContextUsage([{ seq: 1, type: "text", content: "hi" }])).toBeUndefined();
  });
});

describe("contextUsagePercent", () => {
  it("needs a known window", () => {
    expect(contextUsagePercent({ usedTokens: 50_000 })).toBeUndefined();
  });

  it("never rounds the extremes into a state they are not in", () => {
    expect(contextUsagePercent({ usedTokens: 100, limitTokens: 200_000 })).toBe(1);
    expect(contextUsagePercent({ usedTokens: 199_999, limitTokens: 200_000 })).toBe(99);
    expect(contextUsagePercent({ usedTokens: 200_000, limitTokens: 200_000 })).toBe(100);
  });

  it("rounds to whole percent in between", () => {
    expect(contextUsagePercent({ usedTokens: 112_400, limitTokens: 200_000 })).toBe(56);
  });
});

describe("isContextUsageItem", () => {
  it("separates superseded snapshots from events worth a row", () => {
    expect(isContextUsageItem(contextItem(1, { used_tokens: 1_000 }))).toBe(true);
    expect(isContextUsageItem(contextItem(2, { phase: "compacted" }))).toBe(false);
    expect(isContextUsageItem({ seq: 3, type: "text", content: "hi" })).toBe(false);
  });
});

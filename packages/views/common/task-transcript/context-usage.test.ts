// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineItem } from "./build-timeline";
import {
  contextUsageHistory,
  taskContextUsage,
  contextUsagePercent,
  isContextUsageItem,
  latestContextUsage,
  readContextEvent,
} from "./context-usage";

function contextItem(seq: number, input: Record<string, unknown>): TimelineItem {
  return { seq, type: "context", input, content: "Context 1/2" };
}

function textItem(seq: number): TimelineItem {
  return { seq, type: "text", content: "hi" };
}

describe("readContextEvent", () => {
  it("reads an occupancy observation", () => {
    expect(
      readContextEvent(
        contextItem(1, { used_tokens: 112_400, limit_tokens: 200_000, model: "claude-opus-5" }),
      ),
    ).toEqual({
      usedTokens: 112_400,
      limitTokens: 200_000,
      model: "claude-opus-5",
    });
  });

  // A figure with no window is not an observation: only Claude and Codex
  // report a window, and the runtimes that reported occupancy alone have
  // been dropped — their stored events must not come back as a bare number.
  it("rejects an occupancy with no window to measure it against", () => {
    expect(readContextEvent(contextItem(1, { used_tokens: 500 }))).toBeNull();
    expect(readContextEvent(contextItem(1, { used_tokens: 500, limit_tokens: 0 }))).toBeNull();
  });

  it("ignores other event types and payloads with no usable figure", () => {
    expect(readContextEvent(textItem(1))).toBeNull();
    expect(readContextEvent(contextItem(1, {}))).toBeNull();
    expect(readContextEvent(contextItem(1, { used_tokens: "many" }))).toBeNull();
    // What an older daemon's compaction event looks like now: a context row
    // with no occupancy in it, which reads as nothing at all.
    expect(readContextEvent(contextItem(1, { phase: "compacted", pre_tokens: 184_000 }))).toBeNull();
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

  it("skips a trailing event that carries no occupancy of its own", () => {
    const items: TimelineItem[] = [
      contextItem(1, { used_tokens: 180_000, limit_tokens: 200_000 }),
      contextItem(2, { phase: "compacted", pre_tokens: 180_000 }),
    ];
    expect(latestContextUsage(items)?.usedTokens).toBe(180_000);
  });

  it("is undefined when the runtime reported nothing", () => {
    expect(latestContextUsage([textItem(1)])).toBeUndefined();
  });
});

describe("contextUsagePercent", () => {
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
  it("claims every context row, readable or not — none of them is a step", () => {
    expect(isContextUsageItem(contextItem(1, { used_tokens: 1_000 }))).toBe(true);
    // An older daemon's event this client cannot read is still not a step:
    // left in, it would render as an empty row.
    expect(isContextUsageItem(contextItem(2, { phase: "compacted" }))).toBe(true);
    expect(isContextUsageItem(textItem(3))).toBe(false);
  });
});

describe("contextUsageHistory", () => {
  it("keeps every reported change, oldest first, with its time", () => {
    const items: TimelineItem[] = [
      { ...contextItem(1, { used_tokens: 10_000, limit_tokens: 200_000 }), created_at: "2026-11-20T01:00:00Z" },
      textItem(2),
      { ...contextItem(3, { phase: "compacted", pre_tokens: 180_000 }), created_at: "2026-11-20T01:10:00Z" },
      { ...contextItem(4, { used_tokens: 40_000, limit_tokens: 200_000 }), created_at: "2026-11-20T01:11:00Z" },
    ];

    // The unreadable row in the middle contributes nothing rather than an
    // entry with no figure in it.
    expect(contextUsageHistory(items)).toEqual([
      { usedTokens: 10_000, limitTokens: 200_000, model: undefined, at: "2026-11-20T01:00:00Z" },
      { usedTokens: 40_000, limitTokens: 200_000, model: undefined, at: "2026-11-20T01:11:00Z" },
    ]);
  });

  it("is empty for a run whose runtime reported nothing", () => {
    expect(contextUsageHistory([textItem(1)])).toEqual([]);
  });

  // The backend re-states a measurement when the runtime answers with its
  // real window after the fact. That is a correction to one observation, not
  // a second one.
  it("collapses a re-stated measurement into the corrected window", () => {
    const items: TimelineItem[] = [
      { ...contextItem(1, { used_tokens: 120_000, limit_tokens: 200_000 }), created_at: "2026-11-23T01:00:00Z" },
      { ...contextItem(2, { used_tokens: 120_000, limit_tokens: 1_000_000 }), created_at: "2026-11-23T01:00:01Z" },
      { ...contextItem(3, { used_tokens: 240_000, limit_tokens: 1_000_000 }), created_at: "2026-11-23T01:05:00Z" },
    ];
    expect(contextUsageHistory(items)).toEqual([
      { usedTokens: 120_000, limitTokens: 1_000_000, model: undefined, at: "2026-11-23T01:00:01Z" },
      { usedTokens: 240_000, limitTokens: 1_000_000, model: undefined, at: "2026-11-23T01:05:00Z" },
    ]);
  });
});

describe("taskContextUsage", () => {
  it("reads the figure a task row carries", () => {
    expect(taskContextUsage({ context_usage: { used_tokens: 75_200, limit_tokens: 258_400 } }))
      .toEqual({ usedTokens: 75_200, limitTokens: 258_400 });
  });

  // The column is nullable and rows written before the window requirement
  // still carry a bare figure; it renders as no chip rather than a number
  // with nothing to compare it to.
  it("ignores a stored figure with no window", () => {
    expect(taskContextUsage({ context_usage: { used_tokens: 75_200 } })).toBeUndefined();
    expect(taskContextUsage({ context_usage: { used_tokens: 75_200, limit_tokens: 0 } }))
      .toBeUndefined();
  });

  it("is undefined when the runtime reported nothing", () => {
    expect(taskContextUsage({})).toBeUndefined();
    expect(taskContextUsage({ context_usage: { used_tokens: 0 } })).toBeUndefined();
  });
});

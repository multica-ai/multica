// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types/events";
import {
  appendTimelineItem,
  buildTimeline,
  coalesceTimelineItems,
  isOutputTruncated,
  type TimelineItem,
} from "./build-timeline";

function message(seq: number, type: TaskMessagePayload["type"], content?: string): TaskMessagePayload {
  return {
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type,
    content,
  };
}

describe("task transcript timeline", () => {
  it("merges adjacent text and thinking fragments split by streaming flushes", () => {
    const items = buildTimeline([
      message(2, "text", "world"),
      message(1, "text", "hello "),
      message(3, "thinking", "step "),
      message(4, "thinking", "one"),
    ]);

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "hello world" }),
      expect.objectContaining({ seq: 3, type: "thinking", content: "step one" }),
    ]);
  });

  it("does not merge across tool or error boundaries", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "before" },
      { seq: 2, type: "tool_use", tool: "bash" },
      { seq: 3, type: "text", content: "after" },
      { seq: 4, type: "error", content: "failed" },
      { seq: 5, type: "text", content: "done" },
    ]);

    expect(items.map((item) => item.content ?? item.tool)).toEqual([
      "before",
      "bash",
      "after",
      "failed",
      "done",
    ]);
  });

  it("coalesces newly appended live text with the previous text item", () => {
    const existing: TimelineItem[] = [{ seq: 1, type: "text", content: "hello" }];
    const items = appendTimelineItem(existing, { seq: 2, type: "text", content: " world" });

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "hello world" }),
    ]);
  });

  it("coalesces out-of-order raw text by sequence", () => {
    const existing: TimelineItem[] = [
      { seq: 1, type: "text", content: "A" },
      { seq: 3, type: "text", content: "C" },
    ];
    const items = appendTimelineItem(existing, { seq: 2, type: "text", content: "B" });

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "ABC" }),
    ]);
  });

  it("redacts secrets after adjacent chunks are coalesced", () => {
    const items = buildTimeline([
      message(1, "text", "Authorization: Bearer abc123xyz."),
      message(2, "text", "def456"),
    ]);

    expect(items[0]?.content).toBe("Authorization: Bearer [REDACTED]");
    expect(items[0]?.content).not.toContain("abc123xyz");
    expect(items[0]?.content).not.toContain("def456");
  });

  it("keeps the latest created_at when coalescing streaming fragments", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "hello ", created_at: "2026-06-09T09:00:00.000Z" },
      { seq: 2, type: "text", content: "world", created_at: "2026-06-09T09:00:05.000Z" },
    ]);

    expect(items).toEqual([
      expect.objectContaining({
        seq: 1,
        type: "text",
        content: "hello world",
        created_at: "2026-06-09T09:00:05.000Z",
      }),
    ]);
  });

  it("falls back to the previous created_at when the merged fragment has none", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "hello ", created_at: "2026-06-09T09:00:00.000Z" },
      { seq: 2, type: "text", content: "world" },
    ]);

    expect(items[0]?.created_at).toBe("2026-06-09T09:00:00.000Z");
  });
});

describe("tool output completeness", () => {
  function result(output: string | undefined, output_truncated?: boolean): TimelineItem {
    return { seq: 1, type: "tool_result", tool: "bash", output, output_truncated };
  }

  it("carries the server's truncation flag onto the timeline", () => {
    const items = buildTimeline([
      { ...message(1, "tool_result"), output: "cut here", output_truncated: true },
      { ...message(2, "tool_result"), output: "all of it", output_truncated: false },
      { ...message(3, "tool_result"), output: "who knows" },
    ]);

    expect(items.map((i) => i.output_truncated)).toEqual([true, false, undefined]);
  });

  // false is a measurement, undefined is the absence of one. Only a positive
  // measurement earns the per-step remark.
  it("marks only an output measured as truncated", () => {
    expect(isOutputTruncated(result("x", true))).toBe(true);
    expect(isOutputTruncated(result("x", false))).toBe(false);
    expect(isOutputTruncated(result("x"))).toBe(false);
  });

  // A truncated preview keeps the first 8 KiB, so an empty output cannot be one.
  it("says nothing about an empty output", () => {
    expect(isOutputTruncated(result("", true))).toBe(false);
    expect(isOutputTruncated(result(undefined, true))).toBe(false);
  });

  // The flag only describes tool output; prose and thinking have no preview
  // budget to overflow.
  it("ignores message types that have no tool output", () => {
    const others: TimelineItem[] = [
      { seq: 1, type: "text", content: "hello" },
      { seq: 2, type: "thinking", content: "hmm" },
      { seq: 3, type: "error", content: "boom" },
    ];
    expect(others.some(isOutputTruncated)).toBe(false);
  });
});

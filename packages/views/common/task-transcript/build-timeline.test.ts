import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { appendTimelineItem, buildTimeline, coalesceTimelineItems, type TimelineItem } from "./build-timeline";

function message(seq: number, type: TaskMessagePayload["type"], content?: string): TaskMessagePayload {
  return {
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type,
    content,
  };
}

function toolUse(seq: number, tool: string, input: Record<string, unknown>): TaskMessagePayload {
  return {
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type: "tool_use",
    tool,
    input,
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

  it("redacts secrets inside tool input values, covering summary and JSON", () => {
    // Migrating execution history to the chat renderer means tool `input`
    // is rendered both as a raw summary field and as pretty-printed JSON.
    // buildTimeline must mask the value at the source so neither path leaks
    // a secret that the old log dialog would have redacted.
    const items = buildTimeline([
      toolUse(1, "bash", {
        command: "deploy",
        env: { OPENAI_API_KEY: "sk-abcdef0123456789abcdef" },
      }),
    ]);

    const input = items[0]?.input as Record<string, unknown>;
    const env = input.env as Record<string, unknown>;
    expect(env.OPENAI_API_KEY).toBe("[REDACTED API KEY]");

    const serialized = JSON.stringify(items[0]?.input);
    expect(serialized).not.toContain("sk-abcdef0123456789abcdef");
  });

  it("redacts a secret surfaced as the tool summary string", () => {
    // getToolSummary reads `input.command` directly; a secret embedded in a
    // command must already be masked by the time the chat renderer reads it.
    const items = buildTimeline([
      toolUse(1, "bash", {
        command: "curl -H 'Authorization: Bearer supersecrettoken123'",
      }),
    ]);

    const input = items[0]?.input as Record<string, unknown>;
    expect(String(input.command)).not.toContain("supersecrettoken123");
    expect(String(input.command)).toContain("Bearer [REDACTED]");
  });

  it("does not mutate the original message input", () => {
    const original = { secret: "sk-abcdef0123456789abcdef" };
    buildTimeline([toolUse(1, "bash", original)]);
    expect(original.secret).toBe("sk-abcdef0123456789abcdef");
  });
});

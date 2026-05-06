import { describe, it, expect } from "vitest";
import type { ChatMessage } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { splitTimeline, extractCopyText } from "./copy-text";

const text = (seq: number, content: string): ChatTimelineItem => ({
  seq,
  type: "text",
  content,
});

const thinking = (seq: number, content = "..."): ChatTimelineItem => ({
  seq,
  type: "thinking",
  content,
});

const tool = (seq: number, name = "Read"): ChatTimelineItem => ({
  seq,
  type: "tool_use",
  tool: name,
  input: { path: "/x" },
});

const message = (content: string): ChatMessage => ({
  id: "m1",
  chat_session_id: "s1",
  role: "assistant",
  content,
  task_id: "t1",
  created_at: "2026-05-06T00:00:00Z",
});

describe("splitTimeline", () => {
  it("treats an all-text timeline as final (no fold)", () => {
    const items = [text(1, "hello"), text(2, "world")];
    expect(splitTimeline(items)).toEqual({
      preface: [],
      middle: [],
      final: items,
    });
  });

  it("treats an all-non-text timeline as middle with no final", () => {
    const items = [thinking(1), tool(2), thinking(3)];
    const out = splitTimeline(items);
    expect(out.preface).toEqual([]);
    expect(out.middle).toEqual(items);
    expect(out.final).toEqual([]);
  });

  it("standard shape: thinking → tool → text → tool → final-text", () => {
    const t1 = thinking(1);
    const u1 = tool(2);
    const x1 = text(3, "intermediate");
    const u2 = tool(4);
    const f1 = text(5, "final answer");
    const out = splitTimeline([t1, u1, x1, u2, f1]);
    expect(out.preface).toEqual([]);
    expect(out.middle).toEqual([t1, u1, x1, u2]);
    expect(out.final).toEqual([f1]);
  });

  it("collects multiple trailing text segments into final", () => {
    const u = tool(1);
    const f1 = text(2, "para 1");
    const f2 = text(3, "para 2");
    const out = splitTimeline([u, f1, f2]);
    expect(out.middle).toEqual([u]);
    expect(out.final).toEqual([f1, f2]);
  });

  it("collects leading text into preface", () => {
    const p = text(1, "preface");
    const u = tool(2);
    const f = text(3, "final");
    const out = splitTimeline([p, u, f]);
    expect(out.preface).toEqual([p]);
    expect(out.middle).toEqual([u]);
    expect(out.final).toEqual([f]);
  });
});

describe("extractCopyText", () => {
  it("falls back to message.content when timeline is empty (legacy)", () => {
    expect(extractCopyText(message("legacy body"), [])).toBe("legacy body");
  });

  it("returns concatenated text segments for an all-text timeline", () => {
    expect(
      extractCopyText(message(""), [text(1, "hello"), text(2, "world")]),
    ).toBe("hello\n\nworld");
  });

  it("returns only the final text for the standard tool-using shape", () => {
    expect(
      extractCopyText(message(""), [
        thinking(1),
        tool(2),
        text(3, "intermediate — should be excluded"),
        tool(4),
        text(5, "final answer"),
      ]),
    ).toBe("final answer");
  });

  it("includes preface and final, excludes middle text", () => {
    expect(
      extractCopyText(message(""), [
        text(1, "preface"),
        tool(2),
        text(3, "middle — excluded"),
        tool(4),
        text(5, "final"),
      ]),
    ).toBe("preface\n\nfinal");
  });

  it("falls back to message.content when timeline has no text items", () => {
    expect(
      extractCopyText(message("fallback body"), [thinking(1), tool(2)]),
    ).toBe("fallback body");
  });

  it("joins multiple trailing text segments with blank-line separators", () => {
    expect(
      extractCopyText(message(""), [
        tool(1),
        text(2, "para 1"),
        text(3, "para 2"),
      ]),
    ).toBe("para 1\n\npara 2");
  });
});

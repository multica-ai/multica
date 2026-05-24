import { describe, it, expect } from "vitest";
import type { TimelineItem } from "./build-timeline";
import { mergeStreamingChunks } from "./step-grouping";

const thinking = (seq: number, content = "..."): TimelineItem => ({
  seq,
  type: "thinking",
  content,
});

const text = (seq: number, content = "..."): TimelineItem => ({
  seq,
  type: "text",
  content,
});

const tool = (seq: number, name = "Read"): TimelineItem => ({
  seq,
  type: "tool_use",
  tool: name,
  input: { path: "/x" },
});

const result = (seq: number, output = "ok"): TimelineItem => ({
  seq,
  type: "tool_result",
  tool: "Read",
  output,
});

describe("mergeStreamingChunks", () => {
  it("returns empty for empty input", () => {
    expect(mergeStreamingChunks([])).toEqual([]);
  });

  it("returns single item unchanged", () => {
    const out = mergeStreamingChunks([thinking(1, "hello")]);
    expect(out).toEqual([{ ...thinking(1, "hello"), mergedCount: 1 }]);
  });

  it("merges consecutive thinking chunks into one row", () => {
    const out = mergeStreamingChunks([
      thinking(1, "step 1"),
      thinking(2, " step 2"),
      thinking(3, " step 3"),
    ]);
    expect(out).toEqual([
      {
        seq: 1,
        type: "thinking",
        content: "step 1 step 2 step 3",
        mergedCount: 3,
        lastSeq: 3,
      },
    ]);
  });

  it("merges consecutive text chunks into one row", () => {
    const out = mergeStreamingChunks([
      text(1, "hello"),
      text(2, " world"),
    ]);
    expect(out).toEqual([
      {
        seq: 1,
        type: "text",
        content: "hello world",
        mergedCount: 2,
        lastSeq: 2,
      },
    ]);
  });

  it("does NOT merge tool_use events", () => {
    const out = mergeStreamingChunks([tool(1), tool(2)]);
    expect(out).toEqual([
      { ...tool(1), mergedCount: 1 },
      { ...tool(2), mergedCount: 1 },
    ]);
  });

  it("does NOT merge tool_result events", () => {
    const out = mergeStreamingChunks([result(1), result(2)]);
    expect(out).toEqual([
      { ...result(1), mergedCount: 1 },
      { ...result(2), mergedCount: 1 },
    ]);
  });

  it("separates thinking and text even if consecutive", () => {
    const out = mergeStreamingChunks([
      thinking(1, "hmm"),
      text(2, "answer"),
    ]);
    expect(out).toEqual([
      { ...thinking(1, "hmm"), mergedCount: 1 },
      { ...text(2, "answer"), mergedCount: 1 },
    ]);
  });

  it("handles mixed sequence", () => {
    const out = mergeStreamingChunks([
      thinking(1, "step 1"),
      thinking(2, " step 2"),
      tool(3, "Read"),
      tool(4, "Write"),
      text(5, "here "),
      text(6, "is the answer"),
    ]);
    expect(out).toEqual([
      { seq: 1, type: "thinking", content: "step 1 step 2", mergedCount: 2, lastSeq: 2 },
      { ...tool(3, "Read"), mergedCount: 1 },
      { ...tool(4, "Write"), mergedCount: 1 },
      { seq: 5, type: "text", content: "here is the answer", mergedCount: 2, lastSeq: 6 },
    ]);
  });

  it("separates two thinking groups with tool call between them", () => {
    const out = mergeStreamingChunks([
      thinking(1, "first "),
      thinking(2, "part"),
      tool(3),
      thinking(4, "second "),
      thinking(5, "part"),
    ]);
    expect(out).toEqual([
      { seq: 1, type: "thinking", content: "first part", mergedCount: 2, lastSeq: 2 },
      { ...tool(3), mergedCount: 1 },
      { seq: 4, type: "thinking", content: "second part", mergedCount: 2, lastSeq: 5 },
    ]);
  });
});

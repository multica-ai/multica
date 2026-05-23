import { describe, it, expect } from "vitest";
import type { ChatTimelineItem } from "@multica/core/chat";
import { groupConsecutiveSteps } from "./step-grouping";

const text = (seq: number, content = "..."): ChatTimelineItem => ({
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

const result = (seq: number, output = "ok"): ChatTimelineItem => ({
  seq,
  type: "tool_result",
  tool: "Read",
  output,
});

const error = (seq: number, content = "fail"): ChatTimelineItem => ({
  seq,
  type: "error",
  content,
});

describe("groupConsecutiveSteps", () => {
  it("returns empty for empty input", () => {
    expect(groupConsecutiveSteps([])).toEqual([]);
  });

  it("returns a single for one item", () => {
    const out = groupConsecutiveSteps([thinking(1)]);
    expect(out).toEqual([{ kind: "single", item: thinking(1) }]);
  });

  it("groups two consecutive same-type items", () => {
    const out = groupConsecutiveSteps([thinking(1), thinking(2)]);
    expect(out).toEqual([
      { kind: "group", type: "thinking", items: [thinking(1), thinking(2)] },
    ]);
  });

  it("groups 5 consecutive thinking steps", () => {
    const items = [thinking(1), thinking(2), thinking(3), thinking(4), thinking(5)];
    const out = groupConsecutiveSteps(items);
    expect(out).toEqual([
      { kind: "group", type: "thinking", items },
    ]);
  });

  it("does not group different types", () => {
    const out = groupConsecutiveSteps([thinking(1), tool(2)]);
    expect(out).toEqual([
      { kind: "single", item: thinking(1) },
      { kind: "single", item: tool(2) },
    ]);
  });

  it("creates multiple groups for alternating runs", () => {
    const out = groupConsecutiveSteps([
      thinking(1),
      thinking(2),
      tool(3),
      tool(4),
      tool(5),
      thinking(6),
    ]);
    expect(out).toEqual([
      { kind: "group", type: "thinking", items: [thinking(1), thinking(2)] },
      { kind: "group", type: "tool_use", items: [tool(3), tool(4), tool(5)] },
      { kind: "single", item: thinking(6) },
    ]);
  });

  it("treats text items as singles (never grouped)", () => {
    const out = groupConsecutiveSteps([text(1), text(2)]);
    expect(out).toEqual([
      { kind: "single", item: text(1) },
      { kind: "single", item: text(2) },
    ]);
  });

  it("groups tool_results", () => {
    const out = groupConsecutiveSteps([result(1), result(2), result(3)]);
    expect(out).toEqual([
      { kind: "group", type: "tool_result", items: [result(1), result(2), result(3)] },
    ]);
  });

  it("groups errors", () => {
    const out = groupConsecutiveSteps([error(1), error(2)]);
    expect(out).toEqual([
      { kind: "group", type: "error", items: [error(1), error(2)] },
    ]);
  });

  it("handles mixed sequence with text breaks", () => {
    const out = groupConsecutiveSteps([
      thinking(1),
      text(2, "intermediate"),
      thinking(3),
      tool(4),
      tool(5),
    ]);
    expect(out).toEqual([
      { kind: "single", item: thinking(1) },
      { kind: "single", item: text(2, "intermediate") },
      { kind: "single", item: thinking(3) },
      { kind: "group", type: "tool_use", items: [tool(4), tool(5)] },
    ]);
  });
});

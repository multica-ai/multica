// @vitest-environment node
import { describe, expect, it } from "vitest";
import { TaskMessageListSchema, TaskMessagePayloadSchema } from "./schemas";

// The truncation fields decide whether a tool result is presented to the reader
// as complete or as a preview, so a malformed value must never be able to pass
// for a real one. The failure mode being guarded is specific: a string "false"
// is truthy in JavaScript, so a naive check would read it as "truncated" while
// looking entirely deliberate in the code.
describe("TaskMessagePayloadSchema truncation fields", () => {
  it("keeps the three states distinct", () => {
    const unknown = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "tool_result",
      output: "x",
    });
    expect(unknown.output_truncated).toBeUndefined();
    expect(unknown.output_original_bytes).toBeUndefined();

    const complete = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "tool_result",
      output: "x",
      output_truncated: false,
      output_original_bytes: 0,
    });
    expect(complete.output_truncated).toBe(false);
    expect(complete.output_original_bytes).toBe(0);

    const truncated = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "tool_result",
      output: "x",
      output_truncated: true,
      output_original_bytes: 1048576,
    });
    expect(truncated.output_truncated).toBe(true);
    expect(truncated.output_original_bytes).toBe(1048576);
  });

  it.each([
    ["string false", "false"],
    ["string true", "true"],
    ["number", 1],
    ["null", null],
    ["object", {}],
  ])("degrades a %s truncation flag to unknown", (_label, value) => {
    const parsed = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "tool_result",
      output: "x",
      output_truncated: value,
    });
    // Not merely "not true" — it must be undefined, so the UI routes it to the
    // unknown branch rather than asserting the output is complete.
    expect(parsed.output_truncated).toBeUndefined();
  });

  it.each([
    ["negative", -1],
    ["fractional", 1.5],
    ["NaN", Number.NaN],
    ["Infinity", Number.POSITIVE_INFINITY],
    ["numeric string", "1024"],
  ])("degrades a %s byte count to unknown", (_label, value) => {
    const parsed = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "tool_result",
      output: "x",
      output_original_bytes: value,
    });
    expect(parsed.output_original_bytes).toBeUndefined();
  });

  it("keeps a message whose truncation field is malformed", () => {
    // Recovery has to be per-field. Dropping the message — or the whole list —
    // because one field was bad would lose transcript content that is
    // otherwise perfectly readable, which is worse than reporting one unknown.
    const parsed = TaskMessageListSchema.parse([
      { task_id: "t1", seq: 1, type: "tool_result", output: "first", output_truncated: "false" },
      { task_id: "t1", seq: 2, type: "tool_result", output: "second", output_truncated: true },
    ]);
    expect(parsed).toHaveLength(2);
    expect(parsed[0]?.output).toBe("first");
    expect(parsed[0]?.output_truncated).toBeUndefined();
    expect(parsed[1]?.output_truncated).toBe(true);
  });

  it("downgrades an unrecognised message type instead of failing", () => {
    const parsed = TaskMessagePayloadSchema.parse({
      task_id: "t1",
      seq: 1,
      type: "some_future_type",
      output: "x",
    });
    expect(parsed.type).toBe("text");
  });

  it("returns an empty list for a non-array response", () => {
    expect(TaskMessageListSchema.safeParse({ messages: [] }).success).toBe(false);
    expect(TaskMessageListSchema.parse(undefined)).toEqual([]);
  });
});

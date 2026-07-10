import { describe, expect, it } from "vitest";
import { normalizeTaskTranscriptMessages, safeTaskText } from "./task-transcript-safety";

describe("task transcript safety", () => {
  it("turns object-shaped task text into a readable string", () => {
    expect(safeTaskText({ note: "hello" })).toBe("{\"note\":\"hello\"}");
  });

  it("normalizes malformed message fields", () => {
    const [message] = normalizeTaskTranscriptMessages([
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: Number.NaN,
        type: "tool_result",
        tool: { name: "Bash" } as unknown as string,
        output: { ok: true } as unknown as string,
      },
    ]);

    expect(message).toEqual(
      expect.objectContaining({
        seq: 1,
        type: "tool_result",
        tool: undefined,
        output: "{\"ok\":true}",
      }),
    );
  });
});

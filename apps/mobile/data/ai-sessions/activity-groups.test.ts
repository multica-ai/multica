// @vitest-environment node
import { expect, it } from "vitest";
import { groupTranscriptActivities, type TranscriptRow } from "./projection";

it("groups consecutive diagnostics without hiding messages, errors or turn boundaries", () => {
  const rows: TranscriptRow[] = [
    { id: "u", turnId: "t1", role: "user", content: "Question" },
    { id: "r", turnId: "t1", role: "reasoning", content: "Summary" },
    { id: "c", turnId: "t1", role: "tool", content: "Tool output" },
    { id: "a", turnId: "t1", role: "assistant", content: "Progress" },
    { id: "c2", turnId: "t1", role: "tool", content: "More output" },
    { id: "e", turnId: "t1", role: "tool", content: "Visible error", error: true },
    { id: "c3", turnId: "t2", role: "tool", content: "Next turn" },
  ];
  const result = groupTranscriptActivities(rows);
  expect(result.map((row) => row.role)).toEqual(["user", "activity", "assistant", "activity", "error", "activity"]);
  expect(result[1]).toMatchObject({ id: "r:activity", items: [rows[1], rows[2]] });
  const streamed = groupTranscriptActivities([...rows.slice(0, 3), { id: "next", turnId: "t1", role: "reasoning", content: "", running: true }]);
  expect(streamed[1]).toMatchObject({ id: "r:activity", items: expect.arrayContaining([expect.objectContaining({ id: "next", running: true })]) });
  expect(rows).toHaveLength(7);
});

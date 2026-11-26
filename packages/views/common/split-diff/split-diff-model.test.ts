// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseUnifiedDiff } from "../task-transcript/trace-event-presenter";
import { buildSplitDiff, splitDiffFromTraceLines } from "./split-diff-model";

describe("splitDiffFromTraceLines", () => {
  it("numbers rows from the hunk header and pairs word-level edits", () => {
    const items = splitDiffFromTraceLines(
      parseUnifiedDiff("@@ -10,1 +10,1 @@ helper\n-hello world\n+hello there\n"),
    );

    expect(items[0]).toMatchObject({
      type: "hunk",
      hunk: { oldStart: 10, oldCount: 1, newStart: 10, newCount: 1 },
      preview: "helper",
    });
    const row = items[1];
    expect(row?.type).toBe("row");
    if (row?.type !== "row") return;
    expect(row.row.leftLine).toBe(10);
    expect(row.row.rightLine).toBe(10);
    expect(row.row.left.kind).toBe("remove");
    expect(row.row.right.kind).toBe("add");
    expect(row.row.left.parts).toEqual([
      { text: "hello ", type: "equal" },
      { text: "world", type: "remove" },
    ]);
    expect(row.row.right.parts).toEqual([
      { text: "hello ", type: "equal" },
      { text: "there", type: "add" },
    ]);
  });

  it("does not treat a unified patch as added file content", () => {
    const unified = "@@ -1,1 +1,2 @@\n-old\n+new\n+extra\n";
    const items = splitDiffFromTraceLines(parseUnifiedDiff(unified));
    const rows = items.filter((item) => item.type === "row");

    expect(items.some((item) => item.type === "hunk")).toBe(true);
    expect(
      rows.some(
        (item) => item.type === "row" && item.row.right.kind === "add" && item.row.right.text.startsWith("@@"),
      ),
    ).toBe(false);
    expect(rows.map((item) => (item.type === "row" ? [item.row.left.text, item.row.right.text] : null))).toEqual([
      ["old", "new"],
      ["", "extra"],
    ]);

    const forged = buildSplitDiff("", unified);
    expect(forged.some((row) => row.right.text.startsWith("@@"))).toBe(true);
  });
});

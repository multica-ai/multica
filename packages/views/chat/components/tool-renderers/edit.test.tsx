import { describe, expect, it } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { computeLineDiff, diffStat, DiffBlock } from "@multica/ui/markdown/DiffBlock";
import type { ChatTimelineItem } from "@multica/core/chat";
import { EditToolBody } from "./edit";

function editItem(input: Record<string, unknown>): ChatTimelineItem {
  return { seq: 1, type: "tool_use", tool: "Edit", input, status: "done" };
}

describe("computeLineDiff", () => {
  it("classifies added, removed, and context lines", () => {
    const lines = computeLineDiff("a\nb\nc", "a\nB\nc");
    expect(lines).toEqual([
      { type: "context", text: "a" },
      { type: "del", text: "b" },
      { type: "add", text: "B" },
      { type: "context", text: "c" },
    ]);
    expect(diffStat(lines)).toEqual({ added: 1, removed: 1 });
  });

  it("treats an empty old string as all additions", () => {
    const lines = computeLineDiff("", "x\ny");
    expect(lines.every((l) => l.type === "add")).toBe(true);
    expect(diffStat(lines)).toEqual({ added: 2, removed: 0 });
  });
});

describe("DiffBlock", () => {
  it("renders +/− gutter glyphs and tinted line backgrounds (not color alone)", () => {
    const { container } = render(<DiffBlock oldString="old" newString="new" filePath="/a/b.ts" />);
    // Non-color signalling: an explicit +/− gutter glyph exists for each change.
    expect(container.textContent).toContain("+");
    expect(container.textContent).toContain("−");
    expect(container.querySelector(".bg-success\\/10")).not.toBeNull();
    expect(container.querySelector(".bg-destructive\\/10")).not.toBeNull();
    // Sticky file-path header.
    expect(screen.getByText("/a/b.ts")).toBeInTheDocument();
  });
});

describe("EditToolBody", () => {
  it("shows a +X/−Y summary for an edit with prior text", () => {
    render(<EditToolBody item={editItem({ file_path: "/a/b.ts", old_string: "a\nb", new_string: "a\nB\nc" })} />);
    expect(screen.getByText("+2")).toBeInTheDocument();
    expect(screen.getByText("−1")).toBeInTheDocument();
    // The diff body is expandable.
    expect(screen.getByText("diff")).toBeInTheDocument();
  });

  it("routes the diff into DiffBlock when expanded", () => {
    render(<EditToolBody item={editItem({ file_path: "/a/b.ts", old_string: "a", new_string: "b" })} />);
    act(() => {
      screen.getByText("diff").closest("button")?.click();
    });
    expect(screen.getByText("/a/b.ts")).toBeInTheDocument();
  });

  it("falls back to a labeled block for a written (new) file with no prior text", () => {
    render(<EditToolBody item={editItem({ file_path: "/a/new.ts", content: "export const x = 1;" })} />);
    expect(screen.getByText(/new file/)).toBeInTheDocument();
  });
});

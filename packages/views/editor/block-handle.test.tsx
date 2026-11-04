import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { useRef } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";

vi.mock("../i18n", async () => {
  const editor: Record<string, unknown> = (await import("../locales/en/editor.json")).default;
  const t = (select: (bundle: unknown) => unknown) => {
    const path: string[] = [];
    const recorder: unknown = new Proxy({}, { get: (_, key: string) => (path.push(key), recorder) });
    select(recorder);
    return String(path.reduce<unknown>((node, key) => (node as Record<string, unknown>)?.[key], editor) ?? "");
  };
  return { useT: () => ({ t }) };
});

import { createEditorExtensions } from "./extensions";
import { BlockHandle } from "./block-handle";

let current: Editor | null = null;

beforeAll(() => {
  Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
  Range.prototype.getBoundingClientRect = () => new DOMRect();
});

afterEach(() => {
  current?.destroy();
  current = null;
});

function Harness({ content }: { content: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const editor = useEditor({
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: true,
      slashCommandMode: "block",
      onUploadFileRef: { current: undefined },
    }),
    content,
    contentType: "markdown",
    immediatelyRender: true,
  });
  current = editor;
  return (
    <div ref={containerRef} data-testid="container" className="relative">
      <EditorContent editor={editor} />
      {editor && <BlockHandle editor={editor} containerRef={containerRef} />}
    </div>
  );
}

/** Points the handle at the top-level block starting at `pos`. */
function hoverBlock(editor: Editor, pos: number) {
  editor.view.posAtCoords = () => ({ pos: pos + 1, inside: pos });
  fireEvent.mouseMove(screen.getByTestId("container"), { clientY: 10 });
}

function blockPositions(editor: Editor): number[] {
  const positions: number[] = [];
  editor.state.doc.forEach((_, offset) => positions.push(offset));
  return positions;
}

describe("BlockHandle", () => {
  it("appears for the hovered block and duplicates it", async () => {
    render(<Harness content={"First\n\nSecond"} />);
    const editor = current!;
    hoverBlock(editor, blockPositions(editor)[1]!);

    await userEvent.click(await screen.findByRole("button", { name: "Drag to move, click for options" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Duplicate" }));

    expect(editor.getMarkdown().trim()).toBe("First\n\nSecond\n\nSecond");
  });

  it("deletes the hovered block", async () => {
    render(<Harness content={"First\n\nSecond"} />);
    const editor = current!;
    hoverBlock(editor, blockPositions(editor)[0]!);

    await userEvent.click(await screen.findByRole("button", { name: "Drag to move, click for options" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));

    expect(editor.getMarkdown().trim()).toBe("Second");
  });

  it("turns a paragraph into a heading", async () => {
    render(<Harness content={"First\n\nSecond"} />);
    const editor = current!;
    hoverBlock(editor, blockPositions(editor)[1]!);

    await userEvent.click(await screen.findByRole("button", { name: "Drag to move, click for options" }));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Heading 2" }));

    expect(editor.getMarkdown().trim()).toBe("First\n\n## Second");
  });

  it("offers no turn-into for a non-text block", async () => {
    render(<Harness content={"```\ncode\n```"} />);
    const editor = current!;
    hoverBlock(editor, 0);

    await userEvent.click(await screen.findByRole("button", { name: "Drag to move, click for options" }));
    expect(await screen.findByRole("menuitem", { name: "Duplicate" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Heading 2" })).toBeNull();
  });

  it("starts a move drag of the whole block", () => {
    render(<Harness content={"First\n\nSecond"} />);
    const editor = current!;
    hoverBlock(editor, blockPositions(editor)[1]!);
    const handle = screen.getByRole("button", { name: "Drag to move, click for options" });

    const data = new Map<string, string>();
    const dataTransfer = {
      clearData: () => data.clear(),
      setData: (type: string, value: string) => data.set(type, value),
      setDragImage: vi.fn(),
      effectAllowed: "",
    };
    act(() => {
      fireEvent.dragStart(handle, { dataTransfer });
    });

    expect(editor.view.dragging?.move).toBe(true);
    expect(editor.view.dragging?.slice.content.firstChild?.textContent).toBe("Second");
    expect(data.get("text/plain")).toContain("Second");

    act(() => {
      fireEvent.dragEnd(handle);
    });
    expect(editor.view.dragging).toBeNull();
  });

  it("hides when the document changes under it", () => {
    render(<Harness content={"First\n\nSecond"} />);
    const editor = current!;
    hoverBlock(editor, 0);
    expect(screen.getByRole("button", { name: "Drag to move, click for options" })).toBeInTheDocument();

    act(() => {
      editor.commands.insertContentAt(1, "x");
    });
    expect(screen.queryByRole("button", { name: "Drag to move, click for options" })).toBeNull();
  });
});

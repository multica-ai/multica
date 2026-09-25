import { act, fireEvent, render, screen } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { beforeEach, describe, expect, it, vi } from "vitest";

const formatState = vi.hoisted(() => ({ codeBlock: false }));

vi.mock("@floating-ui/dom", () => ({
  autoUpdate: vi.fn(() => vi.fn()),
  computePosition: vi.fn(() =>
    Promise.resolve({
      x: 0,
      y: 0,
      middlewareData: {},
    }),
  ),
  flip: vi.fn(),
  hide: vi.fn(),
  offset: vi.fn(),
  shift: vi.fn(),
}));

vi.mock("@tiptap/react", () => ({
  useEditorState: () => ({
    bold: false,
    italic: false,
    strike: false,
    code: false,
    codeBlock: formatState.codeBlock,
    highlight: false,
    link: false,
    blockquote: false,
    bulletList: false,
    orderedList: false,
    taskList: false,
    headingLevel: undefined,
  }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({
    mutateAsync: vi.fn(),
  }),
}));

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (selector: (resources: typeof editor) => string) => selector(editor),
    }),
  };
});

import { EditorBubbleMenu } from "./bubble-menu";

function createEditor(codeBlock = false): Editor {
  const chain = {
    focus: vi.fn(),
    toggleBold: vi.fn(),
    toggleItalic: vi.fn(),
    toggleStrike: vi.fn(),
    toggleCode: vi.fn(),
    toggleHighlight: vi.fn(),
    extendMarkRange: vi.fn(),
    unsetLink: vi.fn(),
    setLink: vi.fn(),
    toggleHeading: vi.fn(),
    setParagraph: vi.fn(),
    toggleBulletList: vi.fn(),
    toggleOrderedList: vi.fn(),
    toggleTaskList: vi.fn(),
    toggleBlockquote: vi.fn(),
    insertContentAt: vi.fn(),
    run: vi.fn(),
  };
  for (const method of Object.keys(chain)) {
    if (method !== "run") {
      chain[method as keyof typeof chain] = vi.fn(() => chain) as never;
    }
  }

  return {
    isEditable: true,
    isDestroyed: false,
    isInitialized: false,
    state: {
      selection: { empty: false, from: 1, to: 2 },
      doc: {
        textBetween: () => "selected text",
        resolve: () => ({ parent: { type: { name: codeBlock ? "codeBlock" : "paragraph" } } }),
      },
    },
    view: {
      dom: document.createElement("div"),
      hasFocus: () => false,
    },
    commands: {
      focus: vi.fn(),
      setTextSelection: vi.fn(),
    },
    chain: () => chain,
    getAttributes: () => ({ href: "https://example.com" }),
    on: vi.fn(),
    off: vi.fn(),
  } as unknown as Editor;
}

describe("EditorBubbleMenu accessibility", () => {
  beforeEach(() => { vi.clearAllMocks(); formatState.codeBlock = false; });

  it("keeps the selection and toolbar on failed capture without resubscribing for new action objects", () => {
    const editor = createEditor();
    Object.defineProperty(editor, "isInitialized", { value: true });
    const { rerender } = render(<EditorBubbleMenu editor={editor} selectionAction={{ label: "Add to comment", onSelect: () => false }} />);
    const transaction = vi.mocked(editor.on).mock.calls.find(([event]) => event === "transaction")?.[1] as (() => void);
    act(() => transaction());
    const registrations = vi.mocked(editor.on).mock.calls.filter(([event]) => event === "transaction").length;
    rerender(<EditorBubbleMenu editor={editor} selectionAction={{ label: "Add to comment", onSelect: () => false }} />);
    expect(vi.mocked(editor.on).mock.calls.filter(([event]) => event === "transaction")).toHaveLength(registrations);
    fireEvent.click(screen.getByRole("button", { name: "Add to comment" }));
    expect(editor.commands.setTextSelection).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Add to comment" })).toBeVisible();
  });

  it.each([false, true])("exposes a non-editing selection action, including code (code: %s)", (codeBlock) => {
    formatState.codeBlock = codeBlock;
    const editor = createEditor(codeBlock);
    Object.defineProperty(editor, "isInitialized", { value: true });
    const onSelect = vi.fn();
    render(<EditorBubbleMenu editor={editor} selectionAction={{ label: "Add to comment", onSelect }} />);
    const transaction = vi.mocked(editor.on).mock.calls.find(([event]) => event === "transaction")?.[1] as (() => void);
    act(() => transaction());
    const action = screen.getByRole("button", { name: "Add to comment" });
    expect(action).toBeVisible();
    fireEvent.mouseDown(action);
    fireEvent.click(action);
    expect(onSelect).toHaveBeenCalledOnce();
    expect(editor.commands.setTextSelection).toHaveBeenCalledWith(2);
    expect(editor.chain().insertContentAt).not.toHaveBeenCalled();
    expect(editor.chain().toggleBlockquote).not.toHaveBeenCalled();
    if (codeBlock) expect(screen.queryByLabelText("Bold")).not.toBeInTheDocument();
  });

  it("gives every icon-only formatting control an accessible name", () => {
    render(
      <EditorBubbleMenu editor={createEditor()} currentIssueId="issue-parent" />,
    );

    for (const name of [
      "Bold",
      "Italic",
      "Strikethrough",
      "Code",
      "Highlight",
      "Link",
      "List",
      "Task list",
      "Quote",
      "Create sub-issue from selection",
    ]) {
      expect(
        screen.getByLabelText(name, { selector: "button" }),
      ).toBeInTheDocument();
    }
  });

  it("gives every icon-only link editing control an accessible name", () => {
    render(<EditorBubbleMenu editor={createEditor()} />);

    fireEvent.click(screen.getByLabelText("Link", { selector: "button" }));

    expect(
      screen.getByLabelText("Apply link", { selector: "button" }),
    ).toBeInTheDocument();
    expect(
      screen.getByLabelText("Remove link", { selector: "button" }),
    ).toBeInTheDocument();
    expect(
      screen.getByLabelText("Close link editor", { selector: "button" }),
    ).toBeInTheDocument();
  });
});

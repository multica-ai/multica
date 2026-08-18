import { fireEvent, render, screen } from "@testing-library/react";
import type { Editor } from "@tiptap/core";
import { beforeEach, describe, expect, it, vi } from "vitest";

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

function createEditor(): Editor {
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
        resolve: () => ({ parent: { type: { name: "paragraph" } } }),
      },
    },
    view: {
      dom: document.createElement("div"),
      hasFocus: () => false,
    },
    commands: {
      focus: vi.fn(),
    },
    chain: () => chain,
    getAttributes: () => ({ href: "https://example.com" }),
    on: vi.fn(),
    off: vi.fn(),
  } as unknown as Editor;
}

describe("EditorBubbleMenu accessibility", () => {
  beforeEach(() => vi.clearAllMocks());

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

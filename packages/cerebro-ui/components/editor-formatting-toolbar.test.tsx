// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EditorFormattingToolbar } from "./editor-formatting-toolbar";

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({
      user: {
        preferences: {
          cerebro_editor_toolbar_order: ["italic", "bold"],
        },
      },
      setUser: vi.fn(),
    }),
}));

const run = vi.fn(() => true);
const focus = vi.fn();
const toggleBold = vi.fn();
const toggleItalic = vi.fn();

function fakeEditor() {
  const chain = {
    focus: () => {
      focus();
      return chain;
    },
    toggleBold: () => {
      toggleBold();
      return chain;
    },
    toggleItalic: () => {
      toggleItalic();
      return chain;
    },
    toggleStrike: () => chain,
    toggleHighlight: () => chain,
    toggleTaskList: () => chain,
    toggleBulletList: () => chain,
    toggleOrderedList: () => chain,
    toggleBlockquote: () => chain,
    toggleCode: () => chain,
    sinkListItem: () => chain,
    liftListItem: () => chain,
    setParagraph: () => chain,
    toggleHeading: () => chain,
    extendMarkRange: () => chain,
    setLink: () => chain,
    unsetLink: () => chain,
    run,
  };

  return {
    isEditable: true,
    isActive: () => false,
    chain: () => chain,
    can: () => ({
      sinkListItem: () => true,
      liftListItem: () => true,
    }),
    on: vi.fn(),
    off: vi.fn(),
    getAttributes: () => ({}),
    state: {
      selection: { empty: true, from: 1, to: 1 },
      doc: { textBetween: () => "" },
    },
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("EditorFormattingToolbar", () => {
  it("stays visible without a text selection, follows the saved order, and formats the editor", () => {
    render(
      <EditorFormattingToolbar
        editor={fakeEditor() as never}
      />,
    );

    const toolbar = screen.getByRole("toolbar", {
      name: "Formatting toolbar",
    });
    const italic = screen.getByRole("button", { name: "Italic" });
    const bold = screen.getByRole("button", { name: "Bold" });

    expect(toolbar).toBeVisible();
    expect(
      italic.compareDocumentPosition(bold) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Code" })).toBeVisible();

    fireEvent.click(bold);
    expect(toggleBold).toHaveBeenCalled();
    expect(run).toHaveBeenCalled();
  });
});

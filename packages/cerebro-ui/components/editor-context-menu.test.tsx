// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { describe, it, expect, afterEach } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { Editor } from "@tiptap/react";

afterEach(cleanup);

import {
  EditorContextMenu,
  buildEditorContextItems,
  buildBlockContextItems,
} from "./editor-context-menu";

// The component reads a few things off the editor: whether the selection is
// empty, the selected/block text, whether the block-action commands are
// registered (track B, flag-gated), and the two lifecycle subscriptions. A stub
// keeps the test on the menu's own behaviour instead of a whole Tiptap instance.
function fakeEditor(selected: boolean, blockCommands = false): Editor {
  return {
    state: {
      selection: {
        empty: !selected,
        from: 0,
        to: 3,
        $from: { parent: { textContent: "block text" } },
      },
      doc: { textBetween: () => "abc" },
    },
    // duplicateBlock is present only once track B has registered
    // CerebroBlockActions, which happens only when the toolbar flag is on.
    commands: blockCommands ? { duplicateBlock: () => true } : {},
    on: () => {},
    off: () => {},
  } as unknown as Editor;
}

describe("buildEditorContextItems (FIR-4028 slice 8)", () => {
  it("puts Comment first — it is the reason the menu exists", () => {
    const items = buildEditorContextItems({
      onComment: () => {},
      onCreateIssue: () => {},
    });
    expect(items[0]?.key).toBe("comment");
  });

  it("keeps the designed order of the remaining actions", () => {
    const items = buildEditorContextItems({
      onComment: () => {},
      onCreateIssue: () => {},
    });
    expect(items.map((i) => i.key)).toEqual([
      "comment",
      "cut",
      "copy",
      "paste-plain",
      "turn-into",
      "create-issue",
    ]);
  });

  it("drops the actions the surface did not supply, rather than showing dead items", () => {
    const items = buildEditorContextItems({});
    expect(items.map((i) => i.key)).toEqual([
      "cut",
      "copy",
      "paste-plain",
      "turn-into",
    ]);
  });

  it("offers the block types under Turn into", () => {
    const items = buildEditorContextItems({});
    const turnInto = items.find((i) => i.key === "turn-into");
    expect(turnInto?.options?.map((o) => o.key)).toEqual([
      "paragraph",
      "heading-1",
      "heading-2",
      "heading-3",
      "bulletList",
      "orderedList",
      "blockquote",
      "codeBlock",
    ]);
  });
});

describe("buildBlockContextItems (FIR-4028 slice 8 — no selection)", () => {
  it("mirrors the Phase 6 block menu order", () => {
    const items = buildBlockContextItems({
      onComment: () => {},
      onCreateIssue: () => {},
    });
    expect(items.map((i) => i.key)).toEqual([
      "turn-into",
      "indent",
      "outdent",
      "duplicate",
      "comment",
      "create-issue",
      "delete",
    ]);
  });

  it("offers the block types under Turn into, including Task list", () => {
    const items = buildBlockContextItems({});
    const turnInto = items.find((i) => i.key === "turn-into");
    expect(turnInto?.options?.map((o) => o.key)).toEqual([
      "paragraph",
      "heading-1",
      "heading-2",
      "heading-3",
      "bulletList",
      "orderedList",
      "taskList",
      "blockquote",
      "codeBlock",
    ]);
  });

  it("drops Comment and Create issue when the surface did not supply them", () => {
    const items = buildBlockContextItems({});
    expect(items.map((i) => i.key)).toEqual([
      "turn-into",
      "indent",
      "outdent",
      "duplicate",
      "delete",
    ]);
  });
});

describe("EditorContextMenu", () => {
  it("always renders the editor it wraps, with or without an editor instance", () => {
    render(
      <EditorContextMenu editor={null}>
        <div data-testid="editor">body</div>
      </EditorContextMenu>,
    );
    expect(screen.getByTestId("editor")).toBeInTheDocument();
  });

  // The shared ContextMenuTrigger ships `select-none`, and `user-select` is
  // inherited — wrapping the editor in it stopped text being selectable with
  // the mouse anywhere in a note, which is the one thing this menu needs.
  it("leaves the wrapped editor's text selectable", () => {
    render(
      <EditorContextMenu editor={fakeEditor(false)} onComment={() => {}}>
        <div data-testid="editor">body</div>
      </EditorContextMenu>,
    );
    const trigger = screen
      .getByTestId("editor")
      .closest('[data-slot="context-menu-trigger"]');
    expect(trigger).not.toBeNull();
    expect(trigger?.className).toContain("select-text");
    expect(trigger?.className).not.toContain("select-none");
  });

  // Base UI's context menu calls preventDefault on a right-click inside the
  // trigger — that is what suppresses the browser's own menu. So the question
  // "does the platform menu still open?" is exactly "was the event left
  // unprevented?".
  function rightClick(selected: boolean, blockCommands = false) {
    render(
      <EditorContextMenu
        editor={fakeEditor(selected, blockCommands)}
        onComment={() => {}}
      >
        <div data-testid="editor">body</div>
      </EditorContextMenu>,
    );
    const event = new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
    });
    screen.getByTestId("editor").dispatchEvent(event);
    return event.defaultPrevented;
  }

  it("leaves the browser's own menu in place while nothing is selected and the block menu is off", () => {
    expect(rightClick(false, false)).toBe(false);
  });

  it("opens our block menu on a right-click with no selection once the block actions are registered", () => {
    expect(rightClick(false, true)).toBe(true);
  });

  it("takes over the right-click once text is selected", () => {
    expect(rightClick(true)).toBe(true);
  });

  it("falls back to the browser's native menu on Shift+right-click, so spellcheck and dictation stay reachable", () => {
    render(
      <EditorContextMenu editor={fakeEditor(true)} onComment={() => {}}>
        <div data-testid="editor">body</div>
      </EditorContextMenu>,
    );
    // Shift is tracked from the keyboard so `disabled` is already set when the
    // contextmenu fires — Base UI reads it synchronously.
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Shift" }));
    });
    const event = new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
    });
    screen.getByTestId("editor").dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keyup", { key: "Shift" }));
    });
  });
});

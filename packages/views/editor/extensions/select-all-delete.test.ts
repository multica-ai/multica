import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { Document } from "@tiptap/extension-document";
import { Paragraph } from "@tiptap/extension-paragraph";
import { Text } from "@tiptap/extension-text";
import { AllSelection, TextSelection } from "@tiptap/pm/state";
import { createEditorExtensions } from ".";

// Regression guard for MUL-7725. Before @tiptap/core 3.29, deleting an
// `AllSelection` kept the selection "all" over the emptied document, which
// the browser painted as a lone selected space with no caret. The fix is
// upstream; these tests pin it through our production keymaps.

let editor: Editor;

afterEach(() => {
  editor?.destroy();
  document.body.innerHTML = "";
});

function mount(extensions: Editor["options"]["extensions"], markdown: string) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions,
    content: markdown,
    contentType: "markdown",
  });
}

function mountContentEditor(markdown: string) {
  mount(
    createEditorExtensions({
      disableMentions: true,
      onUploadFileRef: { current: undefined },
    }),
    markdown,
  );
}

function press(key: string) {
  const event = new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
  return event.defaultPrevented;
}

describe("select all → delete", () => {
  it.each(["Backspace", "Delete"])(
    "leaves a caret in the emptied document on %s",
    (key) => {
      mountContentEditor("hello world\n\n- one\n- two");
      editor.commands.selectAll();
      expect(editor.state.selection).toBeInstanceOf(AllSelection);

      expect(press(key)).toBe(true);

      expect(editor.isEmpty).toBe(true);
      expect(editor.state.selection).toBeInstanceOf(TextSelection);
      expect(editor.state.selection.empty).toBe(true);
      expect(editor.state.selection.from).toBe(1);
    },
  );

  it("leaves a caret in the single-line title editor schema too", () => {
    mount(
      [Document.extend({ content: "paragraph" }), Paragraph, Text],
      "Issue title",
    );
    editor.commands.selectAll();

    expect(press("Backspace")).toBe(true);

    expect(editor.getText()).toBe("");
    expect(editor.state.selection).toBeInstanceOf(TextSelection);
    expect(editor.state.selection.empty).toBe(true);
  });

  it("keeps the select-all when the change leaves content behind", () => {
    mountContentEditor("hello world");
    editor.commands.selectAll();

    editor.commands.toggleBold();

    expect(editor.getText()).toBe("hello world");
    expect(editor.state.selection).toBeInstanceOf(AllSelection);
  });
});

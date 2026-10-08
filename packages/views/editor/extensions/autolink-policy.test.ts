import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";

let editor: Editor | null = null;

function makeProductionEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);

  return new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
    }),
  });
}

function typeText(ed: Editor, text: string): void {
  for (const char of text) {
    const { from, to } = ed.state.selection;
    const handled = ed.view.someProp("handleTextInput", (handler) =>
      handler(ed.view, from, to, char, () => ed.state.tr),
    );
    if (!handled) {
      ed.view.dispatch(ed.state.tr.insertText(char, from, to));
    }
  }
}

function pastePlainText(ed: Editor, text: string): void {
  const event = new Event("paste", { bubbles: false, cancelable: true });
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [],
      getData: (type: string) => (type === "text/plain" ? text : ""),
    },
  });
  ed.view.dom.dispatchEvent(event);
}

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("editor autolink policy", () => {
  it.each(["ai.md", "build.sh", "main.rs", "app.py", "4399.com"])(
    "keeps bare token %s as plain text",
    (token) => {
      editor = makeProductionEditor();

      typeText(editor, `${token} `);

      expect(editor.getMarkdown().trim()).toBe(token);
    },
  );

  it.each([
    ["http://4399.com", "http://4399.com"],
    ["https://4399.com", "https://4399.com"],
    ["www.4399.com", "https://www.4399.com"],
    ["contact@example.com", "mailto:contact@example.com"],
  ])("auto-links %s", (text, href) => {
    editor = makeProductionEditor();

    typeText(editor, `${text} `);

    expect(editor.getMarkdown().trim()).toBe(`[${text}](${href})`);
  });

  it("does not auto-link unsupported URL schemes", () => {
    editor = makeProductionEditor();

    typeText(editor, "ftp://example.com ");

    expect(editor.getMarkdown().trim()).toBe("ftp://example.com");
  });

  it("does not link an incomplete email during a pause and links it only after completion", () => {
    editor = makeProductionEditor();

    typeText(editor, "contact@example.co");
    expect(editor.getMarkdown().trim()).toBe("contact@example.co");

    typeText(editor, "m ");
    expect(editor.getMarkdown().trim()).toBe(
      "[contact@example.com](mailto:contact@example.com)",
    );
  });

  it("does not split a slowly completed bare domain", () => {
    editor = makeProductionEditor();

    typeText(editor, "test.de");
    expect(editor.getMarkdown().trim()).toBe("test.de");

    typeText(editor, "v ");
    expect(editor.getMarkdown().trim()).toBe("test.dev");
  });

  it("does not turn a selection into a link when a bare domain is pasted", () => {
    editor = makeProductionEditor();
    editor.commands.setContent("selected text", {
      emitUpdate: false,
      contentType: "markdown",
    });
    editor.commands.setTextSelection({ from: 1, to: 14 });

    pastePlainText(editor, "ai.md");

    expect(editor.getMarkdown().trim()).toBe("ai.md");
  });

  it.each([
    ["https://4399.com", "https://4399.com"],
    ["www.4399.com", "https://www.4399.com"],
    ["contact@example.com", "mailto:contact@example.com"],
  ])("turns a selection into a link when %s is pasted", (text, href) => {
    editor = makeProductionEditor();
    editor.commands.setContent("selected text", {
      emitUpdate: false,
      contentType: "markdown",
    });
    editor.commands.setTextSelection({ from: 1, to: 14 });

    pastePlainText(editor, text);

    expect(editor.getMarkdown().trim()).toBe(`[selected text](${href})`);
  });
});

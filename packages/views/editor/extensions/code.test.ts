import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { createEditorExtensions } from ".";

let editor: Editor | null = null;

interface JsonNode {
  marks?: Array<{ type: string }>;
  content?: JsonNode[];
}

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

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

function makeCodePasteRuleEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);

  return new Editor({
    element,
    extensions: [
      StarterKit,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

function typeText(ed: Editor, text: string) {
  for (const ch of text) {
    const { from, to } = ed.state.selection;
    const handled = ed.view.someProp("handleTextInput", (handler) =>
      handler(ed.view, from, to, ch, () => ed.state.tr),
    );
    if (!handled) {
      ed.view.dispatch(ed.state.tr.insertText(ch, from, to));
    }
  }
}

function hasCodeMark(node: JsonNode): boolean {
  if (node.marks?.some((mark) => mark.type === "code")) return true;
  return (node.content ?? []).some(hasCodeMark);
}

describe("inline code input rule", () => {
  it("preserves the character before an inline code shortcut", () => {
    editor = makeProductionEditor();

    typeText(editor, "abcd`123`");

    expect(editor.getText()).toBe("abcd123");
    expect(editor.getMarkdown().trim()).toBe("abcd`123`");
  });

  it("formats inline code at the start of a paragraph", () => {
    editor = makeProductionEditor();

    typeText(editor, "`123`");

    expect(editor.getText()).toBe("123");
    expect(editor.getMarkdown().trim()).toBe("`123`");
  });

  it("does not treat adjacent backticks as a single-backtick code shortcut", () => {
    editor = makeProductionEditor();

    typeText(editor, "``123``");

    expect(editor.getText()).toBe("``123``");
    expect(hasCodeMark(editor.getJSON() as JsonNode)).toBe(false);
  });
});

describe("inline code paste rule", () => {
  it("preserves the character before an inline code paste match", () => {
    editor = makeCodePasteRuleEditor();

    editor.view.dispatch(
      editor.state.tr.insertText("abcd`123`").setMeta("uiEvent", "paste"),
    );

    expect(editor.getText()).toBe("abcd123");
    expect(editor.getMarkdown().trim()).toBe("abcd`123`");
  });
});

describe("fenced code language", () => {
  it("keeps a newly-created unlabelled code block unlabelled in Markdown", () => {
    editor = makeProductionEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "codeBlock",
          content: [{ type: "text", text: "const answer = 42;" }],
        },
      ],
    });

    expect(editor.getMarkdown().trim()).toBe(
      ["```", "const answer = 42;", "```"].join("\n"),
    );
  });
});

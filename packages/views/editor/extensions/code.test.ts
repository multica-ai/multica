import { describe, it, expect, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { PatchedListItem } from "./list-item";
import { PatchedCode, inputRegex, pasteRegex } from "./code";

// Minimal editor mirroring the production inline-code config: StarterKit's
// stock Code mark disabled in favor of PatchedCode, serialized through
// @tiptap/markdown.
function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ listItem: false, code: false }),
      PatchedListItem,
      PatchedCode,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

interface JsonNode {
  type?: string;
  text?: string;
  marks?: { type: string }[];
  content?: JsonNode[];
}

/** Plain text / code-marked runs of the first paragraph, in order. */
function firstParagraphRuns(editor: Editor): { text: string; code: boolean }[] {
  const json = editor.getJSON() as JsonNode;
  const para = (json.content ?? []).find((n) => n.type === "paragraph");
  return (para?.content ?? []).map((n) => ({
    text: n.text ?? "",
    code: (n.marks ?? []).some((m) => m.type === "code"),
  }));
}

const plainText = (runs: { text: string; code: boolean }[]) =>
  runs
    .filter((r) => !r.code)
    .map((r) => r.text)
    .join("");
const codeText = (runs: { text: string; code: boolean }[]) =>
  runs
    .filter((r) => r.code)
    .map((r) => r.text)
    .join("");

// Faithfully simulate typing: each character gets a chance to fire an input
// rule (handleTextInput) before falling back to a plain insert — exactly how
// ProseMirror processes keyboard input. (Same helper as task-list-markdown.test.)
function typeText(ed: Editor, text: string) {
  for (const ch of text) {
    const { from, to } = ed.state.selection;
    const handled = ed.view.someProp("handleTextInput", (f) =>
      f(ed.view, from, to, ch, () => ed.state.tr),
    );
    if (!handled) ed.view.dispatch(ed.state.tr.insertText(ch, from, to));
  }
}

let editor: Editor;
afterEach(() => editor?.destroy());

describe("inline code input rule (PatchedCode)", () => {
  it("does NOT swallow the character before `code` when there is no space (RAS-6)", () => {
    editor = makeEditor();
    typeText(editor, "abc`code`");

    const runs = firstParagraphRuns(editor);
    // The bug turned `abc` into `ab` + <code>code</code>. The fix keeps `abc`.
    expect(plainText(runs)).toBe("abc");
    expect(codeText(runs)).toBe("code");
  });

  it("keeps the leading space intact when there IS a space (regression guard)", () => {
    editor = makeEditor();
    typeText(editor, "ab `code`");

    const runs = firstParagraphRuns(editor);
    expect(plainText(runs)).toBe("ab ");
    expect(codeText(runs)).toBe("code");
  });

  it("works at the very start of a line", () => {
    editor = makeEditor();
    typeText(editor, "`code`");

    const runs = firstParagraphRuns(editor);
    expect(plainText(runs)).toBe("");
    expect(codeText(runs)).toBe("code");
  });

  it("does not fire inside a triple-backtick run (no double-conversion)", () => {
    editor = makeEditor();
    typeText(editor, "a``b`");

    // The closing single backtick after ``b would otherwise be ambiguous; the
    // `(?!`)` / lookbehind guards keep this from being marked as inline code.
    const runs = firstParagraphRuns(editor);
    expect(codeText(runs)).toBe("");
  });
});

describe("inline code regexes do not capture the preceding character", () => {
  it("inputRegex match starts at the opening backtick", () => {
    const m = "abc`code`".match(inputRegex);
    expect(m?.[0]).toBe("`code`");
    expect(m?.[1]).toBe("code");
  });

  it("pasteRegex match starts at the opening backtick", () => {
    const m = "abc`code`".match(new RegExp(pasteRegex.source));
    expect(m?.[0]).toBe("`code`");
    expect(m?.[1]).toBe("code");
  });
});

describe("markdown rendering path is unaffected (the renderer never ate the char)", () => {
  it("setContent('abc`code`') keeps abc and marks code", () => {
    editor = makeEditor();
    editor.commands.setContent("abc`code`", { contentType: "markdown" });

    const runs = firstParagraphRuns(editor);
    expect(plainText(runs)).toBe("abc");
    expect(codeText(runs)).toBe("code");
  });
});

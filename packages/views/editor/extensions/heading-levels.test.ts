import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { createEditorExtensions } from ".";

/**
 * Markdown defines six heading levels; the editor schema must accept all six.
 *
 * Tiptap's Heading renders `h{level}` only for the levels it was configured
 * with and falls back to `levels[0]` for anything else. With the old
 * `levels: [1, 2, 3]`, `#### Identity & Trust` parsed to a level-4 node (the
 * Markdown source and the saved value were always fine) but drew as an `<h1>`,
 * so a fourth-level heading looked like the document title (MUL-6060).
 *
 * These tests exercise the production extension array, not a hand-built one,
 * so narrowing `levels` again fails here.
 */

let editor: Editor | null = null;

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

/** The production load path — ContentEditor mounts markdown the same way. */
function loadMarkdown(ed: Editor, markdown: string): void {
  ed.commands.setContent(markdown, { contentType: "markdown" });
}

const ALL_LEVELS = [
  "# Level one",
  "",
  "## Level two",
  "",
  "### Level three",
  "",
  "#### Level four",
  "",
  "##### Level five",
  "",
  "###### Level six",
].join("\n");

describe("heading levels in the schema", () => {
  it("keeps a heading node at the level the Markdown asked for", () => {
    editor = makeProductionEditor();
    loadMarkdown(editor, ALL_LEVELS);

    const levels = editor
      .getJSON()
      .content?.filter((node) => node.type === "heading")
      .map((node) => node.attrs?.level);

    expect(levels).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("renders # through ###### as h1 through h6", () => {
    editor = makeProductionEditor();
    loadMarkdown(editor, ALL_LEVELS);

    const rendered = Array.from(
      editor.view.dom.querySelectorAll("h1, h2, h3, h4, h5, h6"),
    ).map((el) => [el.tagName.toLowerCase(), el.textContent]);

    expect(rendered).toEqual([
      ["h1", "Level one"],
      ["h2", "Level two"],
      ["h3", "Level three"],
      ["h4", "Level four"],
      ["h5", "Level five"],
      ["h6", "Level six"],
    ]);
  });

  it("renders no extra h1 — the H4-H6 fallback is gone", () => {
    editor = makeProductionEditor();
    loadMarkdown(editor, ALL_LEVELS);

    expect(editor.view.dom.querySelectorAll("h1")).toHaveLength(1);
  });
});

describe("markdown round-trip", () => {
  it("serializes each level back to the same number of #", () => {
    editor = makeProductionEditor();
    loadMarkdown(editor, ALL_LEVELS);

    expect(editor.getMarkdown().trim()).toBe(ALL_LEVELS);
  });

  it("preserves neighbouring levels when an H4 is edited", () => {
    // Opening an issue, typing into a deep heading and saving must not promote
    // it — or its siblings — to another level.
    editor = makeProductionEditor();
    loadMarkdown(editor, "#### Wave 1\n\nBody text\n\n##### Detail");

    const headingEnd = editor.state.doc.child(0).nodeSize - 1;
    editor.commands.insertContentAt(headingEnd, ": Plugin Spine");

    const markdown = editor.getMarkdown();
    expect(markdown).toContain("#### Wave 1: Plugin Spine");
    expect(markdown).toContain("##### Detail");
  });

  it("turns a deep heading into a paragraph without leaving stray #", () => {
    editor = makeProductionEditor();
    loadMarkdown(editor, "###### Deep");
    editor.commands.setTextSelection(2);
    editor.commands.setParagraph();

    expect(editor.getMarkdown().trim()).toBe("Deep");
  });
});

describe("pasted and typed headings", () => {
  it("keeps a pasted <h4> as a level-4 heading", () => {
    // parseHTML derives its rules from the same `levels` list, so an
    // unconfigured level is not merely mis-rendered — the tag is dropped and
    // the text lands in a paragraph.
    editor = makeProductionEditor();
    editor.commands.setContent("<h4>From another editor</h4>", {
      contentType: "html",
    });

    expect(editor.getJSON().content?.[0]).toMatchObject({
      type: "heading",
      attrs: { level: 4 },
    });
    expect(editor.getMarkdown().trim()).toBe("#### From another editor");
  });

  it("promotes typed '#### ' to a heading via the input rule", () => {
    editor = makeProductionEditor();
    typeText(editor, "#### Typed");

    expect(editor.view.dom.querySelector("h4")?.textContent).toBe("Typed");
  });
});

/**
 * Feeds characters through `handleTextInput` first so input rules fire, exactly
 * as ProseMirror processes real keystrokes. `setContent` bypasses them.
 */
function typeText(ed: Editor, text: string): void {
  for (const ch of text) {
    const { from, to } = ed.state.selection;
    const handled = ed.view.someProp("handleTextInput", (f) =>
      f(ed.view, from, to, ch, () => ed.state.tr),
    );
    if (!handled) ed.view.dispatch(ed.state.tr.insertText(ch, from, to));
  }
}

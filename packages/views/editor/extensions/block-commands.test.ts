import { afterEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { TaskItem, TaskList } from "@tiptap/extension-list";
import { Table, TableCell, TableHeader, TableRow } from "@tiptap/extension-table";
import { ImageExtension } from "./index";
import { CodeBlockExtension } from "./code-block";
import { codeLowlight } from "../syntax-highlight";
import {
  SuggestionTriggerArmingExtension,
  isTriggerArmedAt,
} from "./suggestion-trigger-arming";
import {
  canTurnInto,
  deleteBlock,
  duplicateBlock,
  insertBlock,
  openInsertMenuBelow,
  resolveBlockTarget,
  turnBlockInto,
} from "./block-commands";

const editors: Editor[] = [];

function makeEditor(markdown: string) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const editor = new Editor({
    element,
    extensions: [
      StarterKit.configure({ codeBlock: false }),
      CodeBlockExtension.configure({ lowlight: codeLowlight }),
      TaskList,
      TaskItem.configure({ nested: true }),
      Table,
      TableRow,
      TableHeader,
      TableCell,
      ImageExtension,
      Markdown,
      SuggestionTriggerArmingExtension,
    ],
    content: markdown,
    contentType: "markdown",
  });
  editors.push(editor);
  return editor;
}

const md = (editor: Editor) => editor.getMarkdown().trim();

/** Document position of the first occurrence of `text`. */
function posOf(editor: Editor, text: string): number {
  let found = -1;
  editor.state.doc.descendants((node, pos) => {
    if (found !== -1) return false;
    const index = node.isText ? (node.text ?? "").indexOf(text) : -1;
    if (index !== -1) found = pos + index;
    return true;
  });
  if (found === -1) throw new Error(`text not found: ${text}`);
  return found;
}

function targetAt(editor: Editor, text: string) {
  const target = resolveBlockTarget(editor.state.doc, posOf(editor, text));
  if (!target) throw new Error(`no block for ${text}`);
  return target;
}

afterEach(() => {
  for (const editor of editors.splice(0)) editor.destroy();
});

describe("resolveBlockTarget", () => {
  it("returns the top-level block around text", () => {
    const editor = makeEditor("# Title\n\nBody text");
    expect(targetAt(editor, "Body").node.type.name).toBe("paragraph");
    expect(targetAt(editor, "Title").node.type.name).toBe("heading");
  });

  it("returns the innermost list item, so nested items move on their own", () => {
    const editor = makeEditor("- outer\n   - inner");
    const inner = targetAt(editor, "inner");
    expect(inner.node.type.name).toBe("listItem");
    expect(inner.node.textContent).toBe("inner");
  });

  it("treats a whole table as one block", () => {
    const editor = makeEditor("| a | b |\n| --- | --- |\n| c | d |");
    expect(targetAt(editor, "d").node.type.name).toBe("table");
    expect(canTurnInto(targetAt(editor, "d").node)).toBe(false);
  });
});

describe("turnBlockInto", () => {
  it("turns a paragraph into a heading", () => {
    const editor = makeEditor("First\n\nSecond");
    turnBlockInto(editor, targetAt(editor, "Second"), "heading2");
    expect(md(editor)).toBe("First\n\n## Second");
    expect(editor.state.selection.empty).toBe(true);
  });

  it("lifts a list item out of its list when it becomes text", () => {
    const editor = makeEditor("- one\n- two");
    turnBlockInto(editor, targetAt(editor, "two"), "paragraph");
    expect(md(editor)).toBe("- one\n\ntwo");
  });

  it("turns a heading into a task list", () => {
    const editor = makeEditor("## Ship it");
    turnBlockInto(editor, targetAt(editor, "Ship"), "taskList");
    expect(md(editor)).toBe("- [ ] Ship it");
  });
});

describe("duplicateBlock and deleteBlock", () => {
  it("duplicates a block right below itself", () => {
    const editor = makeEditor("Keep\n\nCopy me");
    duplicateBlock(editor, targetAt(editor, "Copy"));
    expect(md(editor)).toBe("Keep\n\nCopy me\n\nCopy me");
  });

  it("deletes only the targeted list item", () => {
    const editor = makeEditor("- one\n- two\n- three");
    deleteBlock(editor, targetAt(editor, "two"));
    expect(md(editor)).toBe("- one\n- three");
  });
});

describe("openInsertMenuBelow", () => {
  it("opens an armed `/` on a new line below a non-empty block", () => {
    const editor = makeEditor("Above\n\nBelow");
    openInsertMenuBelow(editor, targetAt(editor, "Above"));
    expect(md(editor)).toBe("Above\n\n/\n\nBelow");
    const slash = editor.state.selection.from - 1;
    expect(editor.state.doc.textBetween(slash, slash + 1)).toBe("/");
    expect(isTriggerArmedAt(editor, slash)).toBe(true);
  });

  it("reuses an empty line instead of adding another", () => {
    const editor = makeEditor("Above");
    editor.commands.insertContentAt(editor.state.doc.content.size, { type: "paragraph" });
    const blocks = editor.state.doc.childCount;
    const empty = resolveBlockTarget(editor.state.doc, editor.state.doc.content.size - 1)!;
    openInsertMenuBelow(editor, empty);
    expect(editor.state.doc.childCount).toBe(blocks);
    expect(md(editor)).toBe("Above\n\n/");
  });

  it("adds a sibling item below a list item", () => {
    const editor = makeEditor("- one\n- two");
    openInsertMenuBelow(editor, targetAt(editor, "one"));
    expect(md(editor)).toBe("- one\n- /\n- two");
  });
});

describe("insertBlock", () => {
  function typedSlash(editor: Editor, query = "") {
    const end = editor.state.doc.content.size - 1;
    editor.commands.insertContentAt(end, `/${query}`);
    const to = editor.state.selection.from;
    return { from: to - query.length - 1, to };
  }

  it("replaces the typed query with the chosen heading", () => {
    const editor = makeEditor("Intro");
    editor.commands.insertContentAt(editor.state.doc.content.size, { type: "paragraph" });
    const range = typedSlash(editor, "h1");
    insertBlock(editor, range, "heading1");
    editor.commands.insertContent("Plan");
    expect(md(editor)).toBe("Intro\n\n# Plan");
  });

  it("inserts a Mermaid code block", () => {
    const editor = makeEditor("");
    const range = typedSlash(editor, "mer");
    insertBlock(editor, range, "mermaid");
    editor.commands.insertContent("graph TD");
    expect(md(editor)).toBe("```mermaid\ngraph TD\n```");
  });

  it("inserts a table with a header row", () => {
    const editor = makeEditor("");
    insertBlock(editor, typedSlash(editor), "table");
    let table = null as null | { rows: number; header: string };
    editor.state.doc.descendants((node) => {
      if (node.type.name === "table") {
        table = { rows: node.childCount, header: node.firstChild!.firstChild!.type.name };
        return false;
      }
      return true;
    });
    expect(table).toEqual({ rows: 3, header: "tableHeader" });
  });

  it("uploads picked images at the caret", async () => {
    const editor = makeEditor("");
    const file = new File(["png"], "shot.png", { type: "image/png" });
    const upload = vi.fn(async () => ({
      id: "att-1",
      filename: "shot.png",
      content_type: "image/png",
      link: "https://files.test/att-1",
      markdownLink: "https://app.test/api/attachments/att-1/download",
    }));
    URL.createObjectURL = vi.fn(() => "blob:shot");
    URL.revokeObjectURL = vi.fn();
    insertBlock(editor, typedSlash(editor, "img"), "image", {
      upload: upload as never,
      pick: async (accept) => {
        expect(accept).toBe("image/*");
        return [file];
      },
    });
    await vi.waitFor(() => expect(upload).toHaveBeenCalledWith(file, expect.any(String)));
    await vi.waitFor(() =>
      expect(md(editor)).toBe("![shot.png](https://app.test/api/attachments/att-1/download)"),
    );
  });
});

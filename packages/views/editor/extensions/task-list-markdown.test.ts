import { describe, it, expect, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { PatchedListItem, PatchedTaskItem } from "./list-item";
import { TaskList } from "@tiptap/extension-list";

// A minimal editor mirroring the production list config: StarterKit's stock
// ListItem disabled in favor of PatchedListItem, plus the checkbox TaskList /
// TaskItem pair, all serialized through @tiptap/markdown.
function makeEditor() {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ listItem: false }),
      PatchedListItem,
      TaskList,
      PatchedTaskItem,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

interface JsonNode {
  type?: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: JsonNode[];
}

function findAll(node: JsonNode, type: string, acc: JsonNode[] = []): JsonNode[] {
  if (node.type === type) acc.push(node);
  for (const child of node.content ?? []) findAll(child, type, acc);
  return acc;
}

function nodeText(node: JsonNode): string {
  if (node.text !== undefined) return node.text;
  return (node.content ?? []).map(nodeText).join("");
}

function loadMarkdown(editor: Editor, md: string) {
  editor.commands.setContent(md, { contentType: "markdown" });
}

// Faithfully simulate typing: each character gets a chance to fire an input
// rule (handleTextInput) before falling back to a plain insert — exactly how
// ProseMirror processes keyboard input. Lets us exercise the live `[ ] ` /
// `- [ ] ` shortcuts, which setContent (the markdown path) bypasses.
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

describe("task list markdown parsing", () => {
  it("parses `- [ ]` / `- [x]` into a taskList with checked flags", () => {
    editor = makeEditor();
    loadMarkdown(editor, "- [ ] todo\n- [x] done");

    const json = editor.getJSON() as JsonNode;
    const taskLists = findAll(json, "taskList");
    expect(taskLists).toHaveLength(1);

    const items = findAll(json, "taskItem");
    expect(items).toHaveLength(2);
    expect(items[0]!.attrs?.checked).toBe(false);
    expect(nodeText(items[0]!)).toBe("todo");
    expect(items[1]!.attrs?.checked).toBe(true);
    expect(nodeText(items[1]!)).toBe("done");
  });

  it("accepts an uppercase `- [X]` as checked", () => {
    editor = makeEditor();
    loadMarkdown(editor, "- [X] done");

    const items = findAll(editor.getJSON() as JsonNode, "taskItem");
    expect(items).toHaveLength(1);
    expect(items[0]!.attrs?.checked).toBe(true);
  });

  it("leaves a plain bullet as a bulletList, not a taskList", () => {
    editor = makeEditor();
    loadMarkdown(editor, "- just a bullet");

    const json = editor.getJSON() as JsonNode;
    expect(findAll(json, "taskList")).toHaveLength(0);
    expect(findAll(json, "bulletList")).toHaveLength(1);
  });
});

describe("task list markdown serialization", () => {
  it("round-trips checked and unchecked items", () => {
    editor = makeEditor();
    loadMarkdown(editor, "- [ ] todo\n- [x] done");
    expect(editor.getMarkdown().trim()).toBe("- [ ] todo\n- [x] done");
  });

  it("serializes a checkbox toggled in the editor back to `- [x]`", () => {
    editor = makeEditor();
    loadMarkdown(editor, "- [ ] todo");

    // Flip the single task item's checked attr the way the checkbox NodeView does.
    editor.commands.command(({ tr, state }) => {
      state.doc.descendants((node, pos) => {
        if (node.type.name === "taskItem") {
          tr.setNodeMarkup(pos, undefined, { ...node.attrs, checked: true });
        }
        return true;
      });
      return true;
    });

    expect(editor.getMarkdown().trim()).toBe("- [x] todo");
  });
});

describe("task list typing input rules", () => {
  it("converts `[ ] ` typed in a plain paragraph into an unchecked task", () => {
    editor = makeEditor();
    typeText(editor, "[ ] buy milk");
    expect(editor.getMarkdown().trim()).toBe("- [ ] buy milk");
  });

  it("converts `[x] ` into a checked task", () => {
    editor = makeEditor();
    typeText(editor, "[x] shipped");
    expect(editor.getMarkdown().trim()).toBe("- [x] shipped");
  });

  // The GitHub-style flow the feature request showed: `- ` makes a bullet, then
  // `[ ] ` turns that item into a checkbox.
  it("converts `- [ ] ` typed as a bullet into a task", () => {
    editor = makeEditor();
    typeText(editor, "- [ ] write tests");
    expect(editor.getMarkdown().trim()).toBe("- [ ] write tests");
  });

  it("converts `- [x] ` typed as a bullet into a checked task", () => {
    editor = makeEditor();
    typeText(editor, "- [x] done");
    expect(editor.getMarkdown().trim()).toBe("- [x] done");
  });

  it("only converts the current bullet item, leaving siblings as bullets", () => {
    editor = makeEditor();
    typeText(editor, "- apple");
    editor.commands.enter();
    typeText(editor, "[ ] task");
    expect(editor.getMarkdown().trim()).toBe("- apple\n\n- [ ] task");
  });

  it("leaves a plain `- ` bullet alone (no false conversion)", () => {
    editor = makeEditor();
    typeText(editor, "- hello");
    const json = editor.getJSON() as JsonNode;
    expect(findAll(json, "taskList")).toHaveLength(0);
    expect(findAll(json, "bulletList")).toHaveLength(1);
  });
});

import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { TaskList } from "@tiptap/extension-list";
import { PatchedListItem, PatchedTaskItem } from "./extensions/list-item";
import { countTaskItems } from "./cerebro-task-progress";
import {
  resolveHorizontalSwipe,
  SWIPE_THRESHOLD_PX,
} from "./cerebro-list-touch";

interface JsonNode {
  type: string;
  attrs?: Record<string, unknown>;
  text?: string;
  content?: JsonNode[];
}

function makeEditor(content: JsonNode) {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ listItem: false }),
      PatchedListItem,
      TaskList,
      PatchedTaskItem,
    ],
    content,
  });
}

function task(text: string, checked: boolean, children?: JsonNode): JsonNode {
  return {
    type: "taskItem",
    attrs: { checked },
    content: [
      { type: "paragraph", content: [{ type: "text", text }] },
      ...(children ? [children] : []),
    ],
  };
}

function taskList(...items: JsonNode[]): JsonNode {
  return { type: "taskList", content: items };
}

describe("countTaskItems", () => {
  let editor: Editor | undefined;
  afterEach(() => {
    editor?.destroy();
    editor = undefined;
    document.body.innerHTML = "";
  });

  it("counts total and checked items, nested included", () => {
    editor = makeEditor({
      type: "doc",
      content: [
        taskList(
          task("a", true),
          task("b", false, taskList(task("b1", true), task("b2", false))),
        ),
      ],
    });
    expect(countTaskItems(editor.state.doc)).toEqual({ total: 4, done: 2 });
  });

  it("returns zero when the document has no task list", () => {
    editor = makeEditor({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "x" }] }],
    });
    expect(countTaskItems(editor.state.doc)).toEqual({ total: 0, done: 0 });
  });

  it("does not auto-check a parent when its only child is checked", () => {
    // A parent item stays unchecked even though its single child is done —
    // guards the "a parent item is never checked automatically" requirement.
    editor = makeEditor({
      type: "doc",
      content: [taskList(task("parent", false, taskList(task("child", true))))],
    });
    const parent = (editor.getJSON() as JsonNode).content?.[0]?.content?.[0];
    expect(parent?.attrs?.checked).toBe(false);
    expect(countTaskItems(editor.state.doc)).toEqual({ total: 2, done: 1 });
  });
});

describe("resolveHorizontalSwipe", () => {
  it("indents on a rightward swipe past the threshold", () => {
    expect(
      resolveHorizontalSwipe(SWIPE_THRESHOLD_PX + 5, { selecting: false }),
    ).toBe("indent");
  });

  it("outdents on a leftward swipe past the threshold", () => {
    expect(
      resolveHorizontalSwipe(-(SWIPE_THRESHOLD_PX + 5), { selecting: false }),
    ).toBe("outdent");
  });

  it("ignores travel shorter than the threshold", () => {
    expect(
      resolveHorizontalSwipe(SWIPE_THRESHOLD_PX - 1, { selecting: false }),
    ).toBeNull();
  });

  it("is disabled while the user is selecting text", () => {
    expect(
      resolveHorizontalSwipe(SWIPE_THRESHOLD_PX + 50, { selecting: true }),
    ).toBeNull();
  });
});

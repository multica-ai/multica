import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import { TextSelection } from "@tiptap/pm/state";
import { createEditorExtensions } from ".";
import { isTriggerArmedAt } from "./suggestion-trigger-arming";

// MUL-9305. A voice keyboard that records first inserts the whole
// transcription in one `handleTextInput` call. Driven here the way
// prosemirror-view's readDOMChange calls it, against the production extensions.

let editor: Editor | null = null;

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

function makeEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  const ed = new Editor({
    element,
    extensions: createEditorExtensions({
      placeholder: "",
      disableMentions: true,
      enableSlashCommands: false,
      onUploadFileRef: { current: undefined },
    }),
  });
  ed.commands.setContent("<p>已有内容</p>");
  ed.commands.focus("end");
  return ed;
}

function insertText(ed: Editor, text: string): void {
  const { view } = ed;
  const { from, to } = view.state.selection;
  const deflt = () => view.state.tr.insertText(text, from, to);
  if (!view.someProp("handleTextInput", (f) => f(view, from, to, text, deflt))) {
    view.dispatch(deflt());
  }
}

describe("multi-character text input", () => {
  it.each([
    ["今天下午三点开会，记得带电脑。", "已有内容今天下午三点开会，记得带电脑。"],
    // A rule matched only the tail and wrote just its replacement.
    ["把尺寸改成3x4", "已有内容把尺寸改成3×4"],
    // A rule matched only the tail and threw, so nothing was inserted.
    ["然后再看看...", "已有内容然后再看看…"],
    ["中间用--", "已有内容中间用—"],
    ["版权写 (c)", "已有内容版权写 ©"],
    ["运行 `make up`", "已有内容运行 make up"],
  ])("keeps the whole phrase %j", (phrase, expected) => {
    editor = makeEditor();
    expect(() => insertText(editor!, phrase)).not.toThrow();
    expect(editor.getText()).toBe(expected);
  });

  it("replaces a selection with the whole phrase", () => {
    editor = makeEditor();
    const { view } = editor;
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, 1, 5)));
    insertText(editor, "改成3x4");
    expect(editor.getText()).toBe("改成3×4");
  });

  it("leaves single keystrokes to the input rules", () => {
    editor = makeEditor();
    for (const ch of "3x4 a--") insertText(editor, ch);
    expect(editor.getText()).toBe("已有内容3×4 a—");
  });

  it("still arms a trigger character inside the phrase", () => {
    editor = makeEditor();
    const from = editor.state.selection.from;
    insertText(editor, "联系 @张三");
    expect(editor.getText()).toBe("已有内容联系 @张三");
    expect(isTriggerArmedAt(editor, from + 3)).toBe(true);
  });
});

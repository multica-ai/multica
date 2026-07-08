import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import type { RefObject } from "react";
import { BaseMentionExtension } from "./mention-extension";
import {
  createIssueIdentifierAutolinkExtension,
  type IssueIdentifierResolver,
} from "./issue-identifier-autolink";

// The real mention extension renders via a React NodeView (IssueChip → workspace
// hooks) that needs providers we don't mount here. Swap in a plain-DOM NodeView
// so the node still serialises through the inherited renderMarkdown, without
// pulling React into the jsdom editor.
const TestMention = BaseMentionExtension.extend({
  addNodeView() {
    return () => ({ dom: document.createElement("span") });
  },
});

const resolveMock = vi.fn<IssueIdentifierResolver>();
const resolveRef: RefObject<IssueIdentifierResolver | undefined> = {
  current: (identifier) => resolveMock(identifier),
};

let editor: Editor | null = null;

function makeEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ link: false }),
      TestMention,
      createIssueIdentifierAutolinkExtension({ resolveRef }),
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

/** Flush the resolver microtask + the follow-up replacement dispatch. */
function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

/** Simulate user typing `text` at position 1 (empty paragraph start). */
function typeAt1(ed: Editor, text: string): void {
  const tr = ed.state.tr.insertText(text, 1);
  ed.view.dispatch(tr);
}

beforeEach(() => {
  resolveMock.mockReset();
});

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("createIssueIdentifierAutolinkExtension", () => {
  it("converts a completed identifier into a canonical issue mention", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "MUL-1" });
    editor = makeEditor();

    // Typing "MUL-1 " leaves the caret right after the boundary space, which
    // completes the previous token.
    typeAt1(editor, "MUL-1 ");
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("MUL-1");
    expect(editor.getMarkdown().trim()).toBe(
      "[MUL-1](mention://issue/uuid-1)",
    );
  });

  it("leaves an unresolvable identifier as plain text", async () => {
    resolveMock.mockResolvedValue(null);
    editor = makeEditor();

    typeAt1(editor, "MUL-9 ");
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("MUL-9");
    const md = editor.getMarkdown();
    expect(md).toContain("MUL-9");
    expect(md).not.toContain("mention://issue");
  });

  it("converts identifiers found inside pasted text", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-2", identifier: "MUL-2" });
    editor = makeEditor();

    const tr = editor.state.tr.insertText("See MUL-2 now", 1);
    tr.setMeta("paste", true);
    editor.view.dispatch(tr);
    await flush();

    expect(resolveMock).toHaveBeenCalledWith("MUL-2");
    expect(editor.getMarkdown().trim()).toBe(
      "See [MUL-2](mention://issue/uuid-2) now",
    );
  });

  it("does not convert content set programmatically (open ≠ rewrite)", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-3", identifier: "MUL-3" });
    editor = makeEditor();

    // setContent uses emitUpdate:false (preventUpdate) — the same path the real
    // editor uses on mount and WS-driven resets.
    editor.commands.setContent("MUL-3 stays", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).toContain("MUL-3");
    expect(editor.getMarkdown()).not.toContain("mention://issue");
  });

  it("does not convert an identifier inside inline code", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-4", identifier: "MUL-4" });
    editor = makeEditor();

    const codeMark = editor.schema.marks.code!.create();
    const tr = editor.state.tr.insert(
      1,
      editor.schema.text("MUL-4 ", [codeMark]),
    );
    editor.view.dispatch(tr);
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).not.toContain("mention://issue");
  });

  it("does not fire while the identifier is still being typed (no boundary yet)", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-5", identifier: "MUL-5" });
    editor = makeEditor();

    typeAt1(editor, "MUL-5");
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    expect(editor.getMarkdown()).not.toContain("mention://issue");
  });
});

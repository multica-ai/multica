import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import { Markdown } from "@tiptap/markdown";
import type { RefObject } from "react";
import { BaseMentionExtension } from "./mention-extension";
import {
  createIssueIdentifierAutolinkExtension,
  type IssueIdentifierResolver,
} from "./issue-identifier-autolink";

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: false,
});

/** Count canonical issue mentions in serialised markdown. */
function mentionCount(markdown: string): number {
  return markdown.match(/mention:\/\/issue\//g)?.length ?? 0;
}

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
      LinkExtension,
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

  // --- blocker regressions: replace ONLY the captured range ---------------

  it("converts only the newly-typed occurrence, not a pre-existing identical one", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "MUL-1" });
    editor = makeEditor();

    // Pre-existing MUL-1 arrives via a programmatic (preventUpdate) set — the
    // path the real editor uses on mount / WS resets — so it is not captured.
    editor.commands.setContent("old MUL-1 here", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();
    expect(resolveMock).not.toHaveBeenCalled();

    // User now types a brand-new MUL-1 at the end of the paragraph.
    const endOfPara = editor.state.doc.content.size - 1;
    editor.view.dispatch(editor.state.tr.insertText(" MUL-1 ", endOfPara));
    await flush();

    const md = editor.getMarkdown();
    // Exactly the new occurrence became a mention; the old one stays text.
    expect(mentionCount(md)).toBe(1);
    expect(md).toContain("old MUL-1 here");
    expect(md).toContain("mention://issue/uuid-1");
  });

  it("paste converts identifiers inside the paste range but not identical ones outside it", async () => {
    resolveMock.mockImplementation(async (identifier) => {
      const map: Record<string, string> = {
        "MUL-2": "uuid-2",
        "MUL-3": "uuid-3",
        "MUL-9": "uuid-9",
      };
      const id = map[identifier];
      return id ? { id, identifier } : null;
    });
    editor = makeEditor();

    // Pre-existing (programmatic) MUL-9 outside any future paste range.
    editor.commands.setContent("keep MUL-9 outside", {
      emitUpdate: false,
      contentType: "markdown",
    });
    await flush();

    // Paste two identifiers at the very start of the doc.
    const tr = editor.state.tr.insertText("MUL-2 plus MUL-3 x ", 1);
    tr.setMeta("paste", true);
    editor.view.dispatch(tr);
    await flush();

    const md = editor.getMarkdown();
    // Both pasted identifiers converted; the outside MUL-9 did NOT.
    expect(md).toContain("mention://issue/uuid-2");
    expect(md).toContain("mention://issue/uuid-3");
    expect(md).not.toContain("mention://issue/uuid-9");
    expect(md).toContain("MUL-9");
    expect(mentionCount(md)).toBe(2);
    expect(resolveMock).not.toHaveBeenCalledWith("MUL-9");
  });

  it("does not replace an identifier that already carries an explicit link mark", async () => {
    resolveMock.mockResolvedValue({ id: "uuid-1", identifier: "MUL-1" });
    editor = makeEditor();

    // A link-marked "MUL-1" (e.g. an existing markdown link label) followed by
    // plain text, then the user types a boundary after it.
    const linkMark = editor.schema.marks.link!.create({ href: "https://x.test" });
    const linked = editor.schema.text("MUL-1", [linkMark]);
    const trailing = editor.schema.text(" ");
    const paragraph = editor.schema.nodes.paragraph!.create(null, [
      linked,
      trailing,
    ]);
    editor.view.dispatch(
      editor.state.tr.replaceWith(0, editor.state.doc.content.size, paragraph),
    );
    await flush();

    expect(resolveMock).not.toHaveBeenCalled();
    const md = editor.getMarkdown();
    expect(md).not.toContain("mention://issue");
    expect(md).toContain("https://x.test");
  });
});

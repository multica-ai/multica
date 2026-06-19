import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import { Markdown } from "@tiptap/markdown";
import { AutolinkEmailRepairExtension } from "./autolink-email-repair";

const LinkExtension = Link.extend({ inclusive: false }).configure({
  openOnClick: false,
  autolink: true,
  linkOnPaste: true,
  defaultProtocol: "https",
});

let editor: Editor | null = null;

function makeEditor(): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  return new Editor({
    element,
    extensions: [
      StarterKit.configure({ link: false }),
      LinkExtension,
      AutolinkEmailRepairExtension,
      Markdown.configure({ indentation: { style: "space", size: 3 } }),
    ],
  });
}

/**
 * Simulate the split-link scenario: a mailto: link followed by trailing plain
 * text in the same paragraph. This mirrors what happens when autolink fires on
 * `contact@example.co` and the user then types `m`.
 */
function setSplitEmailLink(
  ed: Editor,
  linkText: string,
  trailingText: string,
): void {
  const linkMark = ed.schema.marks.link!.create({
    href: `mailto:${linkText}`,
  });
  const { tr } = ed.state;
  const linkedNode = ed.schema.text(linkText, [linkMark]);
  const trailingNode = ed.schema.text(trailingText);
  const paragraph = ed.schema.nodes.paragraph!.create(null, [
    linkedNode,
    trailingNode,
  ]);
  tr.replaceWith(0, ed.state.doc.content.size, paragraph);
  ed.view.dispatch(tr);
}

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
});

describe("AutolinkEmailRepairExtension", () => {
  it("extends .co link when trailing text completes .com", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "contact@example.co", "m");

    const markdown = editor.getMarkdown().trim();
    expect(markdown).toBe("[contact@example.com](mailto:contact@example.com)");
  });

  it("extends .co link when trailing text completes .co.uk", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "contact@example.co", ".uk");

    const markdown = editor.getMarkdown().trim();
    expect(markdown).toBe(
      "[contact@example.co.uk](mailto:contact@example.co.uk)",
    );
  });

  it("extends .i link when trailing text completes .io", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "user@host.i", "o");

    const markdown = editor.getMarkdown().trim();
    expect(markdown).toBe("[user@host.io](mailto:user@host.io)");
  });

  it("does NOT absorb random text after an email link", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "contact@example.com", " hello");

    const markdown = editor.getMarkdown().trim();
    // The link should stay as-is; " hello" should remain outside.
    expect(markdown).toContain("[contact@example.com](mailto:contact@example.com)");
    expect(markdown).toContain("hello");
    expect(markdown).not.toBe(
      "[contact@example.com hello](mailto:contact@example.com hello)",
    );
  });

  it("does NOT affect regular URL links", () => {
    editor = makeEditor();
    const linkMark = editor.schema.marks.link!.create({
      href: "https://example.co",
    });
    const { tr } = editor.state;
    const linkedNode = editor.schema.text("example.co", [linkMark]);
    const trailingNode = editor.schema.text("m");
    const paragraph = editor.schema.nodes.paragraph!.create(null, [
      linkedNode,
      trailingNode,
    ]);
    tr.replaceWith(0, editor.state.doc.content.size, paragraph);
    editor.view.dispatch(tr);

    const markdown = editor.getMarkdown().trim();
    // The trailing "m" should NOT be absorbed into the URL link.
    expect(markdown).toContain("[example.co](https://example.co)");
    expect(markdown).toMatch(/\)m/);
  });

  it("does NOT extend when no prefix of trailing text forms a valid email", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "contact@example.co", "random");

    const markdown = editor.getMarkdown().trim();
    // "contact@example.corandom" — no prefix of "random" makes a valid email.
    expect(markdown).toContain("[contact@example.co](mailto:contact@example.co)");
    expect(markdown).toContain("random");
  });

  it("finds the longest valid extension (.co + .uk, not just .co + .)", () => {
    editor = makeEditor();
    setSplitEmailLink(editor, "contact@example.co", ".uk extra");

    const markdown = editor.getMarkdown().trim();
    expect(markdown).toContain(
      "[contact@example.co.uk](mailto:contact@example.co.uk)",
    );
    expect(markdown).toContain("extra");
  });
});

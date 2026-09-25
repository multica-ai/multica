import { describe, it, expect, afterEach } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { HighlightExtension } from "./highlight";

let editor: Editor | null = null;

function makeEditor(markdown: string): Editor {
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: [StarterKit, Markdown, HighlightExtension],
  });
  editor.commands.setContent(markdown, { contentType: "markdown" });
  return editor;
}

/** Round-trip: load markdown → serialize back to markdown. */
function roundTrip(markdown: string): string {
  return makeEditor(markdown).getMarkdown().trim();
}

afterEach(() => {
  editor?.destroy();
  editor = null;
});

describe("HighlightExtension — markdown serialization (cross-process protocol)", () => {
  it("round-trips a basic highlight as ==text==", () => {
    expect(roundTrip("==hi==")).toBe("==hi==");
  });

  it("round-trips a highlight embedded in a sentence", () => {
    expect(roundTrip("before ==mid== after")).toBe("before ==mid== after");
  });

  it("parses ==text== into a highlight mark (<mark> in HTML)", () => {
    const html = makeEditor("==hi==").getHTML();
    expect(html).toContain("<mark");
    expect(html).toContain("hi");
  });

  it("preserves inner formatting inside a highlight", () => {
    // Tiptap 3.25+ serializes mark nesting by extension rank, so bold wraps
    // highlight here. The formatting still survives the round-trip.
    expect(roundTrip("==**bold**==")).toBe("**==bold==**");
  });

  it("serializes a highlight applied via the toggleHighlight command", () => {
    const e = makeEditor("hello");
    e.commands.selectAll();
    e.commands.toggleHighlight();
    expect(e.getMarkdown().trim()).toBe("==hello==");
  });

  it("leaves a lone == (comparison) untouched", () => {
    expect(roundTrip("if a == b")).toBe("if a == b");
  });

  it("does not treat == inside inline code as a highlight", () => {
    expect(roundTrip("`a ==b== c`")).toBe("`a ==b== c`");
  });

  // Boundary regressions (Emacs review, PR #3661).

  it("does not let a == inside inline code close the highlight", () => {
    const e = makeEditor("==a `b==c` d==");
    const html = e.getHTML();
    // whole span highlighted; inner `==` stays inside an inline <code>
    expect(html).toContain("<mark");
    expect(html).toContain("<code");
    expect(html).toContain("b==c");
    // must NOT have stopped at the code's `==`
    expect(html).not.toMatch(/<mark[^>]*>a\s*$/);
    expect(e.getMarkdown().trim()).toBe("==a `b==c` d==");
  });

  it("does not highlight across a blank line (two literal paragraphs)", () => {
    const e = makeEditor("==a\n\nb==");
    expect(e.getHTML()).not.toContain("<mark");
    expect(e.getMarkdown().trim()).toBe("==a\n\nb==");
  });

  it("does not highlight across a CRLF blank line", () => {
    const e = makeEditor("==a\r\n\r\nb==");
    expect(e.getHTML()).not.toContain("<mark");
  });

  it("still highlights across a CRLF soft line break", () => {
    const e = makeEditor("==a\r\nb==");
    expect(e.getHTML()).toContain("<mark");
  });
});

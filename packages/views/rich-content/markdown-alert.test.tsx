/**
 * GitHub alert callouts (`> [!NOTE]` and its four siblings) in RichContent.
 *
 * Driven through the real renderer, so the matrix covers the whole chain:
 * remark-breaks shaping the marker line, rehype-sanitize running first, and the
 * blockquote renderer reading the tag rehypeMarkdownAlerts left behind.
 */
import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { Editor } from "@tiptap/core";
import { composeAnnotatedReply } from "@multica/core/drafts/reply-annotation";

vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({
      t: (select: (bundle: typeof editor) => string) => select(editor),
    }),
    useTimeAgo: () => "just now",
  };
});

import { createEditorExtensions } from "../editor/extensions";
import { RichContent } from "./rich-content";

function renderContent(content: string): HTMLElement {
  return render(<RichContent content={content} />).container;
}

function callouts(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(".markdown-alert"));
}

describe("GitHub alert callouts", () => {
  it.each([
    ["NOTE", "note", "Note"],
    ["TIP", "tip", "Tip"],
    ["IMPORTANT", "important", "Important"],
    ["WARNING", "warning", "Warning"],
    ["CAUTION", "caution", "Caution"],
  ])("renders > [!%s] as a callout", (marker, type, label) => {
    const container = renderContent(`> [!${marker}]\n> Read this first.`);

    const [callout] = callouts(container);
    expect(callout?.dataset.alert).toBe(type);
    expect(container.querySelector("blockquote")).toBeNull();

    const title = callout?.querySelector(".markdown-alert-title");
    expect(title?.textContent).toBe(label);
    expect(title?.querySelector("svg")).not.toBeNull();

    // The marker is consumed; only the body follows the title.
    expect(title?.nextElementSibling?.textContent).toBe("Read this first.");
    expect(container.textContent).not.toContain("[!");
  });

  it("gives each type its own accent", () => {
    const accents = ["NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"].map(
      (marker) => callouts(renderContent(`> [!${marker}]\n> body`))[0]?.className,
    );
    expect(new Set(accents).size).toBe(5);
  });

  it("matches the marker case-insensitively", () => {
    const [callout] = callouts(renderContent("> [!warning]\n> body"));
    expect(callout?.dataset.alert).toBe("warning");
  });

  it("keeps every block of the quote body", () => {
    const container = renderContent(
      "> [!IMPORTANT]\n> line one\n> line two\n>\n> - item\n>\n> closing",
    );

    const [callout] = callouts(container);
    const body = Array.from(callout?.children ?? []).slice(1);
    expect(body.map((el) => el.tagName)).toEqual(["P", "UL", "P"]);
    expect(body[0]?.textContent).toBe("line one\nline two");
    expect(body[0]?.querySelector("br")).not.toBeNull();
    expect(body[2]?.textContent).toBe("closing");
  });

  // A file written by Windows tools can lead with a BOM, which the Markdown
  // parser skips before counting source offsets.
  it("still matches after a leading byte-order mark", () => {
    const [callout] = callouts(renderContent("\uFEFF> [!IMPORTANT]\n> body"));
    expect(callout?.dataset.alert).toBe("important");
  });

  it("drops a marker paragraph that holds nothing else", () => {
    const [callout] = callouts(renderContent("> [!TIP]\n>\n> Separate paragraph."));
    const body = Array.from(callout?.children ?? []).slice(1);
    expect(body.map((el) => el.textContent)).toEqual(["Separate paragraph."]);
  });

  // The web editor escapes [ and ] in text on save, so a marker typed there is
  // stored as `\[!IMPORTANT\]`. Pin that with the product editor's own
  // extensions, then require the stored form to render exactly like the
  // literal one an API write keeps.
  it("renders the escaped marker the editor stores as the same callout", () => {
    const literal = "> [!IMPORTANT]\n> Read this first.";
    const editor = new Editor({
      element: document.createElement("div"),
      extensions: createEditorExtensions({ disableMentions: true }),
    });
    editor.commands.setContent(literal, { contentType: "markdown" });
    const stored = editor.getMarkdown().trim();
    editor.destroy();

    expect(stored).toBe("> \\[!IMPORTANT\\]\n> Read this first.");
    const escaped = renderContent(stored);
    expect(callouts(escaped)).toHaveLength(1);
    expect(escaped.innerHTML).toBe(renderContent(literal).innerHTML);
  });
});

describe("blockquotes that stay plain", () => {
  const quotedReply = composeAnnotatedReply("", [
    {
      id: "annotation",
      sourceCommentId: "source",
      sourceActorName: "Agent",
      quote: "[!NOTE]\nQuoted body",
      note: "Replying to the quote.",
      start: 0,
      prefix: "",
      suffix: "",
    },
  ]);

  it.each([
    ["an unknown type", "> [!FOO]\n> body", "[!FOO]"],
    ["a marker that is not on the first line", "> intro\n> [!NOTE]\n> body", "[!NOTE]"],
    ["a quoted-reply snapshot with an entity-encoded marker", quotedReply, "[!NOTE]"],
    ["a marker sharing its line with text", "> [!NOTE] inline\n> body", "[!NOTE] inline"],
    ["a marker sharing its line with markup", "> [!NOTE]**bold**\n> body", "[!NOTE]bold"],
    ["a marker with nothing under it", "> [!NOTE]", "[!NOTE]"],
    ["a marker in a nested quote", "> > [!NOTE]\n> > body", "[!NOTE]"],
    ["a marker in a quote inside a list", "- > [!NOTE]\n  > body", "[!NOTE]"],
    [
      "a marker inside a raw HTML paragraph",
      "<blockquote><p>[!NOTE]</p><p>body</p></blockquote>",
      "[!NOTE]",
    ],
  ])("%s", (_, content, literal) => {
    const container = renderContent(content);

    expect(callouts(container)).toHaveLength(0);
    expect(container.querySelector("blockquote")).not.toBeNull();
    expect(container.textContent).toContain(literal);
  });
});

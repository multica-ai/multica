/**
 * Regression tests for nested ordered lists in ContentEditor ↔ ReadonlyContent.
 *
 * Bug: the default @tiptap/markdown serializer emits nested list items with
 * 2-space indent. remark-gfm / CommonMark (used by ReadonlyContent) requires
 * ≥3 spaces of indent to recognize a nested item under a "1. " marker, so the
 * readonly surface used to flatten nested ordered lists into a single level.
 *
 * Fix: MarkdownExtension is configured with `indentation.size = 3`.
 */

import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { MarkdownExtension } from "./extensions";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/test/issues/${id}` }),
  useWorkspaceSlug: () => "test",
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
}));
vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId }: { issueId: string }) => <span>{issueId}</span>,
}));
vi.mock("./extensions/image-view", () => ({ ImageLightbox: () => null }));
vi.mock("./link-hover-card", () => ({ useLinkHover: () => ({}), LinkHoverCard: () => null }));
vi.mock("./utils/link-handler", () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith("mention://")),
}));

import { ReadonlyContent } from "./readonly-content";

const NESTED_MARKDOWN = [
  "1. Top A",
  "   1. Nested A1",
  "   2. Nested A2",
  "2. Top B",
].join("\n");

describe("ContentEditor nested ordered list round-trip", () => {
  it("serializes nested ordered lists with ≥3-space indent", () => {
    const editor = new Editor({
      extensions: [StarterKit, MarkdownExtension],
      content: NESTED_MARKDOWN,
      contentType: "markdown",
    });
    const md = editor.getMarkdown();
    editor.destroy();

    // Nested item must be indented by ≥3 spaces so CommonMark recognizes it
    // as nested under the "1. " marker (marker width 2 + space = 3).
    expect(md).toMatch(/^1\. Top A\n {3,}1\. Nested A1/);
    expect(md).toMatch(/\n {3,}2\. Nested A2\n2\. Top B/);
  });

  it("survives a full editor → ReadonlyContent round-trip as a nested <ol>", () => {
    const editor = new Editor({
      extensions: [StarterKit, MarkdownExtension],
      content: NESTED_MARKDOWN,
      contentType: "markdown",
    });
    const md = editor.getMarkdown();
    editor.destroy();

    const { container } = render(<ReadonlyContent content={md} />);

    // There should be exactly one outer <ol> and one nested <ol> inside it —
    // not a flat 4-item list.
    const outerLists = container.querySelectorAll(".rich-text-editor > ol");
    expect(outerLists).toHaveLength(1);
    expect(container.querySelectorAll("ol ol")).toHaveLength(1);

    // Outer list has 2 direct <li> children: "Top A" (with nested ol) + "Top B".
    const outer = outerLists[0]!;
    const topItems = outer.querySelectorAll(":scope > li");
    expect(topItems).toHaveLength(2);
    const topA = topItems[0]!;
    const topB = topItems[1]!;
    expect(topA.textContent).toContain("Top A");
    expect(topA.querySelector("ol")).not.toBeNull();
    expect(topB.textContent).toContain("Top B");
  });
});

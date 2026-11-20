import { describe, expect, it } from "vitest";
import type { DocComment } from "@multica/core/types";
import { captureDocAnchor, docAnchorRange, locateDocAnchor } from "./doc-comment-anchors";
import { groupDocThreads } from "./doc-comments-panel";

function editorWith(html: string) {
  const root = document.createElement("div");
  root.className = "ProseMirror";
  root.setAttribute("contenteditable", "true");
  root.innerHTML = html;
  document.body.append(root);
  return root;
}

describe("locateDocAnchor", () => {
  const anchor = { quote: "rehearse", prefix: "Migrations must ", suffix: " a day ahead" };

  it("finds the quote again after the text around it moved", () => {
    const text = "Intro added later.\nMigrations must rehearse a day ahead.";
    expect(locateDocAnchor(text, anchor)).toBe(text.indexOf("rehearse"));
  });

  it("picks the occurrence whose context matches when the quote repeats", () => {
    const text = "Teams rehearse often.\nMigrations must rehearse a day ahead.";
    expect(locateDocAnchor(text, anchor)).toBe(text.lastIndexOf("rehearse"));
  });

  it("reports a quote that was edited away", () => {
    expect(locateDocAnchor("Migrations need a dry run first.", anchor)).toBeNull();
  });
});

describe("captureDocAnchor and docAnchorRange", () => {
  it("round-trips a selection across blocks into a range over the same text", () => {
    const root = editorWith("<p>Before release</p><ul><li>Migrations must rehearse a day ahead</li></ul>");
    const text = root.querySelector("li")!.firstChild!;
    const range = document.createRange();
    range.setStart(text, "Migrations must ".length);
    range.setEnd(text, "Migrations must rehearse".length);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);

    const anchor = captureDocAnchor(root, selection);
    expect(anchor).toEqual({ quote: "rehearse", prefix: "Before release\nMigrations must ", suffix: " a day ahead\n" });
    expect(docAnchorRange(root, anchor!)?.toString()).toBe("rehearse");
    root.remove();
  });
});

describe("groupDocThreads", () => {
  const comment = (id: string, parentId: string | null): DocComment => ({
    id, docId: "d", parentId, authorType: "member", authorId: "u", content: id,
    anchorQuote: parentId ? "" : "quote", anchorPrefix: "", anchorSuffix: "",
    resolvedAt: null, resolvedByType: null, resolvedById: null,
    createdAt: "2026-11-19T00:00:00Z", updatedAt: "2026-11-19T00:00:00Z",
  });

  it("keeps discussions in order with their replies, dropping orphan replies", () => {
    const threads = groupDocThreads([comment("a", null), comment("b", null), comment("a1", "a"), comment("x1", "gone")]);
    expect(threads.map((t) => [t.root.id, t.replies.map((r) => r.id)])).toEqual([["a", ["a1"]], ["b", []]]);
  });
});

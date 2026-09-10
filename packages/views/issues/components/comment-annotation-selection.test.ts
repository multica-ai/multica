import { describe, expect, it } from "vitest";
import { annotationRange, captureCommentSelection, indexCommentText } from "./comment-annotation-selection";

function fixture() {
  document.body.innerHTML = '<div data-annotation-thread="thread"><div data-comment-content="source"><p>Hello <strong>world</strong></p><pre><code>one\ntwo</code><button>Copy</button></pre><p>Last</p></div><div data-comment-content="other">Other reply</div><div data-annotation-thread="nested"><div data-comment-content="nested-source">Nested</div></div></div>';
  const card = document.body.firstElementChild as HTMLElement;
  const root = card.firstElementChild as HTMLElement;
  return { card, root };
}

function select(start: Node, startOffset: number, end: Node, endOffset: number) {
  const range = document.createRange();
  range.setStart(start, startOffset);
  range.setEnd(end, endOffset);
  const selection = window.getSelection()!;
  selection.removeAllRanges(); selection.addRange(range);
  return selection;
}

describe("comment annotation selection", () => {
  it("captures rendered text and code newlines, excludes controls, and restores the same Range", () => {
    const { card, root } = fixture();
    const first = root.querySelector("strong")!.firstChild!;
    const last = root.querySelector("code")!.firstChild!;
    const captured = captureCommentSelection(card, select(first, 0, last, 7))!;
    expect(captured.quote).toBe("world\none\ntwo");
    expect(indexCommentText(root).text).toBe("Hello world\none\ntwo\nLast\n");
    const range = annotationRange(root, { ...captured, id: "a", sourceActorName: "Agent", note: "" })!;
    expect(range.startContainer).toBe(first);
    expect(range.endContainer).toBe(last);
    expect(range.endOffset).toBe(7);
  });

  it("rejects selection spanning comments or owned by a nested card", () => {
    const { card, root } = fixture();
    expect(captureCommentSelection(card, select(root.querySelector("strong")!.firstChild!, 0, card.children[1]!.firstChild!, 5))).toBeNull();
    const nested = card.querySelector('[data-comment-content="nested-source"]')!.firstChild!;
    expect(captureCommentSelection(card, select(nested, 0, nested, 6))).toBeNull();
  });

  it("rejects selections in editors, controls, and whitespace", () => {
    const { card, root } = fixture();
    const control = root.querySelector("button")!.firstChild!;
    expect(captureCommentSelection(card, select(control, 0, control, 4))).toBeNull();
    const first = root.querySelector("p")!.firstChild!;
    expect(captureCommentSelection(card, select(first, 5, first, 6))).toBeNull();
  });
});

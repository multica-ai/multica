import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { collectTextMatches, useInPageFind } from "./use-in-page-find";

function makeRoot(html: string): HTMLElement {
  const root = document.createElement("div");
  root.innerHTML = html;
  return root;
}

describe("collectTextMatches", () => {
  it("returns nothing for an empty query", () => {
    expect(collectTextMatches(makeRoot("<p>hello</p>"), "")).toEqual([]);
  });

  it("returns nothing when the query is not present", () => {
    expect(collectTextMatches(makeRoot("<p>hello</p>"), "zzz")).toEqual([]);
  });

  it("finds a case-insensitive match with node offsets", () => {
    const matches = collectTextMatches(makeRoot("<p>Hello World</p>"), "world");
    expect(matches).toHaveLength(1);
    expect(matches[0]!.node.nodeValue).toBe("Hello World");
    expect(matches[0]!.start).toBe(6);
    expect(matches[0]!.end).toBe(11);
  });

  it("finds every non-overlapping occurrence in one node, in order", () => {
    const matches = collectTextMatches(makeRoot("<p>ababab</p>"), "ab");
    expect(matches.map((m) => m.start)).toEqual([0, 2, 4]);
  });

  it("does not overlap on repeated characters", () => {
    const matches = collectTextMatches(makeRoot("<p>aaaa</p>"), "aa");
    expect(matches.map((m) => m.start)).toEqual([0, 2]);
  });

  it("matches inside separate text nodes but never across an element boundary", () => {
    // Text nodes: "foo ", "bar", " foobar". "foo" hits the first and third,
    // and the "bar" split across <strong> is never joined into a match.
    const root = makeRoot("<p>foo <strong>bar</strong> foobar</p>");
    const matches = collectTextMatches(root, "foo");
    expect(matches).toHaveLength(2);
    expect(collectTextMatches(root, "foobar")).toHaveLength(1);
  });

  it("skips <script> and <style> text", () => {
    const root = makeRoot(
      "<style>needle{}</style><script>needle</script><p>needle</p>",
    );
    const matches = collectTextMatches(root, "needle");
    expect(matches).toHaveLength(1);
    expect(matches[0]!.node.parentElement?.tagName).toBe("P");
  });

  it("skips subtrees marked data-find-ignore (e.g. the find bar itself)", () => {
    const root = makeRoot(
      '<div data-find-ignore><span>needle</span></div><p>needle</p>',
    );
    const matches = collectTextMatches(root, "needle");
    expect(matches).toHaveLength(1);
    expect(matches[0]!.node.parentElement?.tagName).toBe("P");
  });
});

// jsdom implements neither `CSS.highlights` nor `Highlight`, so the hook runs
// here exactly as it would on a browser without the CSS Custom Highlight API.
// This is the regression from the review: counting and prev/next navigation
// must keep working even though nothing gets painted.
describe("useInPageFind without the CSS Custom Highlight API", () => {
  let container: HTMLElement;

  beforeEach(() => {
    // jsdom lays nothing out and doesn't implement Range.getClientRects, which
    // the scroll-to-match path now calls in every browser. Return a single
    // zero-size rect so scrollRangeIntoView no-ops instead of throwing.
    (Range.prototype as { getClientRects: () => DOMRectList }).getClientRects =
      () =>
        [
          { top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0 },
        ] as unknown as DOMRectList;
    container = document.createElement("div");
    container.innerHTML = "<h1>Find me</h1><p>find me twice: find</p>";
    document.body.appendChild(container);
  });

  afterEach(() => {
    delete (Range.prototype as { getClientRects?: unknown }).getClientRects;
    container.remove();
  });

  // Flush the rAF-deferred recompute(s) the open/content effects schedule.
  async function flushFrames(count = 3): Promise<void> {
    for (let i = 0; i < count; i++) {
      await act(
        () =>
          new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
      );
    }
  }

  it("counts matches and steps through them when highlighting is unavailable", async () => {
    const { result } = renderHook(() =>
      useInPageFind({ container, contentKey: 0 }),
    );

    expect(result.current.supported).toBe(false);

    act(() => {
      result.current.openFind();
      result.current.setQuery("find");
    });
    await flushFrames();

    // "find" appears once in the heading and twice in the paragraph.
    expect(result.current.matchCount).toBe(3);
    expect(result.current.activeIndex).toBe(0);

    act(() => result.current.goNext());
    expect(result.current.activeIndex).toBe(1);

    act(() => result.current.goPrev());
    act(() => result.current.goPrev());
    expect(result.current.activeIndex).toBe(2); // wraps past the start

    act(() => result.current.setQuery("absent"));
    await flushFrames();
    expect(result.current.matchCount).toBe(0);
    expect(result.current.activeIndex).toBe(-1);
  });
});

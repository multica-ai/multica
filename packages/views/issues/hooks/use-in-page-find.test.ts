import { describe, expect, it } from "vitest";
import { collectTextMatches } from "./use-in-page-find";

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

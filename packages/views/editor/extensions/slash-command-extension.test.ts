import { describe, expect, it } from "vitest";
import { SlashCommandExtension } from "./slash-command-extension";

const tokenizer = SlashCommandExtension.config.markdownTokenizer!;

const startFn = tokenizer.start as (src: string) => number;
const tokenizeFn = tokenizer.tokenize as (
  src: string,
) => { type: string; raw: string; attributes: Record<string, string> } | undefined;
const renderMarkdown = SlashCommandExtension.config.renderMarkdown as (
  node: { attrs: Record<string, string> },
) => string;
const renderHTML = SlashCommandExtension.config.renderHTML as (
  this: { options: { HTMLAttributes: Record<string, string> } },
  props: {
    node: { attrs: Record<string, string | undefined> };
    HTMLAttributes: Record<string, string>;
  },
) => [string, Record<string, string>, string];

function tokenize(src: string) {
  const start = startFn(src);
  if (start === -1) return undefined;
  return tokenizeFn(src.slice(start));
}

describe("slash command tokenizer", () => {
  it("parses a slash skill link", () => {
    const token = tokenize("[/git-commit](slash://skill/aaa-bbb)");

    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("git-commit");
    expect(token!.attributes.id).toBe("aaa-bbb");
  });

  it("round-trips through renderMarkdown", () => {
    const md = renderMarkdown({
      attrs: { id: "skill-1", label: "deploy" },
    });

    expect(md).toBe("[/deploy](slash://skill/skill-1)");
    expect(tokenize(md)?.attributes).toEqual({
      id: "skill-1",
      label: "deploy",
    });
  });

  it("uses a generic fallback when rendering markdown without a label", () => {
    const md = renderMarkdown({
      attrs: { id: "skill-1" },
    });

    expect(md).toBe("[/?](slash://skill/skill-1)");
  });

  it("does not write an unused slash-specific id attribute", () => {
    const [, attrs, text] = renderHTML.call(
      { options: { HTMLAttributes: { class: "slash-command" } } },
      {
        node: { attrs: { id: "skill-1", label: "deploy" } },
        HTMLAttributes: {},
      },
    );

    expect(attrs).toMatchObject({
      "data-type": "slash-command",
      class: "slash-command",
    });
    expect(attrs).not.toHaveProperty("data-slash-id");
    expect(text).toBe("/deploy");
  });

  it("handles labels with escaped brackets", () => {
    const md = renderMarkdown({
      attrs: { id: "skill-1", label: "deploy[prod]" },
    });

    expect(md).toBe("[/deploy\\[prod\\]](slash://skill/skill-1)");
    expect(tokenize(md)?.attributes.label).toBe("deploy[prod]");
  });

  it.each(["A\\", "ends\\", "a\\]b", "f(x)", "back\\slash"])(
    "round-trips a label containing backslash/parens: %j",
    (label) => {
      // renderMarkdown must escape "\" so a trailing "\" does not swallow the
      // closing "]" under the linear tokenizer (regression guard).
      const md = renderMarkdown({ attrs: { id: "skill-1", label } });
      expect(tokenize(md)?.attributes.label).toBe(label);
    },
  );

  it("does not match ordinary markdown links", () => {
    expect(tokenize("[docs](https://example.com)")).toBeUndefined();
  });

  it("does not match slash action links", () => {
    expect(tokenize("[/deploy](slash://action/deploy)")).toBeUndefined();
  });

  it("rejects an unterminated slash link with escape-pair runs in linear time", () => {
    // Each "\a" pair is ambiguous under (?:\\.|[^\]]) — the pre-fix regex
    // enumerates 2^28 backtrack paths (~10s) before failing. The disjoint
    // char class must fail fast instead.
    const src = `[/${"\\a".repeat(28)}](slash://skill/abc`;

    const t0 = performance.now();
    const token = tokenizeFn(src);
    const elapsed = performance.now() - t0;

    expect(token).toBeUndefined();
    expect(elapsed).toBeLessThan(100);
  });

  it("returns -1 fast from start() when escape-pair runs precede no slash link", () => {
    const src = `[/${"\\a".repeat(28)}] plain text, no slash link`;

    const t0 = performance.now();
    const start = startFn(src);
    const elapsed = performance.now() - t0;

    expect(start).toBe(-1);
    expect(elapsed).toBeLessThan(100);
  });
});

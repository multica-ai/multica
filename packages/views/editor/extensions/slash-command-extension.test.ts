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

  it("does not match ordinary markdown links", () => {
    expect(tokenize("[docs](https://example.com)")).toBeUndefined();
  });

  it("does not match slash action links", () => {
    expect(tokenize("[/deploy](slash://action/deploy)")).toBeUndefined();
  });
});

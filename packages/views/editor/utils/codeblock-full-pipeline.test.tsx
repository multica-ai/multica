import { describe, expect, it } from "vitest";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import remarkMath from "remark-math";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { preprocessLinks } from "@multica/ui/markdown/linkify";
import { preprocessMentionShortcodes } from "@multica/ui/markdown/mentions";

const testSchema = {
  ...defaultSchema,
  protocols: { ...defaultSchema.protocols, href: [...(defaultSchema.protocols?.href ?? []), "mention"] },
  attributes: {
    ...defaultSchema.attributes,
    code: [...(defaultSchema.attributes?.code ?? []), ["className", /^language-/]],
  },
};

function fullPipeline(input: string) {
  const step1 = preprocessMentionShortcodes(input);
  const step2 = preprocessLinks(step1);
  return renderToStaticMarkup(
    <ReactMarkdown
      remarkPlugins={[remarkMath, remarkBreaks, [remarkGfm, { singleTilde: false }]]}
      rehypePlugins={[rehypeRaw, [rehypeSanitize, testSchema]]}
    >
      {step2}
    </ReactMarkdown>
  );
}

describe("Full pipeline: code blocks with URLs/@/#", () => {
  it("backtick-fenced code block with URL has NO <a> inside", () => {
    const md = "```bash\nvisit https://www.wujieai.com\n```";
    const html = fullPipeline(md);
    expect(html).toContain("https://www.wujieai.com");
    expect(html).not.toMatch(/<a\s/);
  });

  it("tilde-fenced code block with URL — check for <a>", () => {
    const md = "~~~bash\nvisit https://www.wujieai.com\n~~~";
    const html = fullPipeline(md);
    console.log("Tilde result:", html);
    expect(html).toContain("https://www.wujieai.com");
    // Check if <a> appears (BUG: preprocessLinks linkifies inside tilde fences)
    const hasLink = /<a\s/.test(html);
    console.log("Has <a> inside tilde-fenced code block:", hasLink);
  });

  it("indented code block with URL — check for <a>", () => {
    const md = "    visit https://www.wujieai.com\n    more code";
    const html = fullPipeline(md);
    console.log("Indented result:", html);
    expect(html).toContain("https://www.wujieai.com");
    const hasLink = /<a\s/.test(html);
    console.log("Has <a> inside indented code block:", hasLink);
  });

  it("code block with @ and # has no interactive elements", () => {
    const md = "```text\n@someone tagged #123 issue\n```";
    const html = fullPipeline(md);
    expect(html).not.toMatch(/<a\s/);
    expect(html).toContain("@someone");
    expect(html).toContain("#123");
  });

  it("regular text URL is properly linkified", () => {
    const md = "Visit https://www.wujieai.com now";
    const html = fullPipeline(md);
    expect(html).toMatch(/<a\s/);
  });
});

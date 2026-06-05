import { describe, expect, it } from "vitest";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";

// Minimal sanitize schema for testing
const testSchema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    code: [...(defaultSchema.attributes?.code ?? []), ["className", /^language-/]],
  },
};

describe("remarkGfm autolink inside code blocks", () => {
  it("should NOT autolink URLs inside fenced code blocks", () => {
    const md = "```bash\nvisit https://www.wujieai.com\n```";
    const html = renderToStaticMarkup(
      <ReactMarkdown remarkPlugins={[[remarkGfm, { singleTilde: false }]]} rehypePlugins={[rehypeRaw, [rehypeSanitize, testSchema]]}>
        {md}
      </ReactMarkdown>
    );
    // Should NOT contain <a> tag inside code block
    expect(html).not.toContain("<a ");
    // Should contain the raw URL as text
    expect(html).toContain("https://www.wujieai.com");
  });

  it("should NOT autolink @ symbols inside code blocks", () => {
    const md = "```text\n@someone said #123\n```";
    const html = renderToStaticMarkup(
      <ReactMarkdown remarkPlugins={[[remarkGfm, { singleTilde: false }]]} rehypePlugins={[rehypeRaw, [rehypeSanitize, testSchema]]}>
        {md}
      </ReactMarkdown>
    );
    expect(html).toContain("@someone");
    expect(html).toContain("#123");
  });

  it("SHOULD autolink URLs in regular text", () => {
    const md = "Visit https://www.wujieai.com for details";
    const html = renderToStaticMarkup(
      <ReactMarkdown remarkPlugins={[[remarkGfm, { singleTilde: false }]]} rehypePlugins={[rehypeRaw, [rehypeSanitize, testSchema]]}>
        {md}
      </ReactMarkdown>
    );
    expect(html).toContain("<a ");
    expect(html).toContain("https://www.wujieai.com");
  });
});

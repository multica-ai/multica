// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import Markdown from "react-markdown";
import rehypeRaw from "rehype-raw";
import { titleLinkPayload } from "./title-link";

describe("titleLinkPayload", () => {
  it("builds a Markdown link and an HTML anchor with the same label and URL", () => {
    expect(
      titleLinkPayload(
        "TES-1 Example",
        "https://app.example/test/issues/TES-1",
      ),
    ).toEqual({
      text: "[TES-1 Example](https://app.example/test/issues/TES-1)",
      html: '<a href="https://app.example/test/issues/TES-1">TES-1 Example</a>',
    });
  });

  it("escapes Markdown link syntax in the label", () => {
    expect(titleLinkPayload("Fix [draft] (v2)", "https://x.test/i/1").text).toBe(
      "[Fix \\[draft\\] \\(v2\\)](https://x.test/i/1)",
    );
  });

  it("escapes HTML text and attribute content", () => {
    const { html } = titleLinkPayload(
      'Ship <b>"fast"</b> & safe',
      'https://x.test/i/1?a=1&b="2"',
    );
    expect(html).toBe(
      '<a href="https://x.test/i/1?a=1&amp;b=&quot;2&quot;">Ship &lt;b&gt;&quot;fast&quot;&lt;/b&gt; &amp; safe</a>',
    );
  });

  it.each(["details", "Enter"])(
    "renders <%s> in the Markdown link as literal title text",
    (tag) => {
      const { text } = titleLinkPayload(
        `MUL-1 Support <${tag}> & text`,
        "https://x.test/i/1",
      );
      const rendered = renderToStaticMarkup(
        createElement(Markdown, { rehypePlugins: [rehypeRaw], children: text }),
      );

      expect(rendered).toBe(
        `<p><a href="https://x.test/i/1">MUL-1 Support &lt;${tag}&gt; &amp; text</a></p>`,
      );
    },
  );
});

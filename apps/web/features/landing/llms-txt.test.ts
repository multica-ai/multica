// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createEnDict } from "./i18n/en";
import { buildLlmsTxt } from "./llms-txt";

const dict = createEnDict();

describe("buildLlmsTxt", () => {
  const text = buildLlmsTxt();

  it("follows the llms.txt shape: title, summary, then sections", () => {
    expect(text.startsWith(`# Multica\n\n> ${dict.meta.home.description}\n`)).toBe(true);
    for (const section of ["Core capabilities", "Get started", "Documentation", "FAQ", "Pages"]) {
      expect(text).toContain(`\n## ${section}\n`);
    }
  });

  it("states the product facts from the site copy", () => {
    for (const item of dict.faq.items) expect(text).toContain(`### ${item.question}`);
    for (const feature of Object.values(dict.home.features.items)) expect(text).toContain(feature.body);
    expect(text).toMatch(/supports \d+ coding tools/);
    expect(text).toContain("Using Multica inside your own organization is free");
  });

  it("links the docs Markdown and the agent install guide", () => {
    expect(text).toContain("(https://www.multica.ai/docs/llms.txt)");
    expect(text).toContain("(https://www.multica.ai/docs/llms-full.txt)");
    expect(text).toContain("(https://www.multica.ai/docs/self-hosting-ai.md)");
  });

  it("makes every link absolute and carries no markup", () => {
    const hrefs = [...text.matchAll(/\]\(([^)]+)\)/g)].map((match) => match[1]);
    expect(hrefs.length).toBeGreaterThan(0);
    for (const href of hrefs) expect(href).toMatch(/^https:\/\//);
    expect(text).not.toMatch(/<\/?[a-z][^>]*>/i);
  });

  it("lists only released pages", () => {
    expect(text).toContain(`- [${dict.meta.download.title}](https://www.multica.ai/download)`);
    const unreleased = buildLlmsTxt(dict, (href) => href !== "/download");
    expect(unreleased).toContain(`- [${dict.meta.about.title}](https://www.multica.ai/about)`);
    expect(unreleased).not.toContain(`- [${dict.meta.download.title}]`);
  });
});

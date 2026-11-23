// @vitest-environment node
import { describe, expect, it } from "vitest";
import robots from "./robots";

describe("robots.txt", () => {
  const { rules, sitemap: sitemaps } = robots();
  const groups = Array.isArray(rules) ? rules : [rules];

  it("welcomes AI search and assistant crawlers to the site, docs and llms.txt", () => {
    const ai = groups.find((group) => Array.isArray(group.userAgent) && group.userAgent.includes("ClaudeBot"));
    expect(ai?.userAgent).toEqual(
      expect.arrayContaining(["ClaudeBot", "Claude-User", "OAI-SearchBot", "ChatGPT-User", "PerplexityBot", "Google-Extended"]),
    );
    expect(ai?.allow).toEqual(expect.arrayContaining(["/", "/docs/", "/llms.txt"]));
  });

  it("keeps private app routes closed to every crawler", () => {
    for (const group of groups) {
      expect(group.disallow).toEqual(expect.arrayContaining(["/api/", "/auth/", "/issues", "/settings"]));
    }
    expect(sitemaps).toEqual(["https://www.multica.ai/sitemap.xml", "https://www.multica.ai/docs/sitemap.xml"]);
  });
});

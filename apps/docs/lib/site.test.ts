import { describe, expect, it, vi } from "vitest";

const pages = new Map<string, { url: string }>([
  ["en:", { url: "/" }],
  ["zh:", { url: "/zh" }],
  ["ko:", { url: "/ko" }],
  ["en:agents", { url: "/agents" }],
  ["zh:agents", { url: "/zh/agents" }],
  ["ko:agents", { url: "/ko/agents" }],
]);

vi.mock("@/lib/source", () => ({
  source: {
    getPage: vi.fn((slugs: string[], lang: string) => {
      return pages.get(`${lang}:${slugs.join("/")}`) ?? null;
    }),
  },
}));

describe("docsAlternates", () => {
  it("omits Korean hreflang when the page only renders via English fallback", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "https://www.multica.ai/docs/agents",
      languages: {
        en: "https://www.multica.ai/docs/agents",
        zh: "https://www.multica.ai/docs/zh/agents",
        "x-default": "https://www.multica.ai/docs/agents",
      },
    });
  });

  it("omits Korean hreflang even when Fumadocs returns a fallback page for Korean", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"]).languages).not.toHaveProperty("ko");
  });

  it("keeps the locale root alternates limited to real localized MDX pages", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates([])).toEqual({
      canonical: "https://www.multica.ai/docs",
      languages: {
        en: "https://www.multica.ai/docs",
        zh: "https://www.multica.ai/docs/zh",
        "x-default": "https://www.multica.ai/docs",
      },
    });
  });
});

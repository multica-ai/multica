import { beforeEach, describe, expect, it, vi } from "vitest";

const existingDocs = vi.hoisted(() => new Set<string>());

vi.mock("node:fs", () => ({
  existsSync: vi.fn((path: string) => {
    const normalized = path.replaceAll("\\", "/");
    return [...existingDocs].some((suffix) => normalized.endsWith(suffix));
  }),
}));

const pages = new Map<string, { url: string }>([
  ["en:", { url: "/" }],
  ["zh:", { url: "/zh" }],
  ["ko:", { url: "/ko" }],
  ["ja:", { url: "/ja" }],
  ["en:agents", { url: "/agents" }],
  ["zh:agents", { url: "/zh/agents" }],
  ["ko:agents", { url: "/ko/agents" }],
  ["ja:agents", { url: "/ja/agents" }],
]);

vi.mock("@/lib/source", () => ({
  source: {
    getPage: vi.fn((slugs: string[], lang: string) => {
      return pages.get(`${lang}:${slugs.join("/")}`) ?? null;
    }),
  },
}));

beforeEach(() => {
  existingDocs.clear();
  existingDocs.add("index.mdx");
  existingDocs.add("index.zh.mdx");
  existingDocs.add("agents.mdx");
  existingDocs.add("agents.zh.mdx");
});

describe("docsAlternates", () => {
  it("omits Korean hreflang when no Korean MDX file exists for the page", async () => {
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

  it("omits Korean hreflang even when source.getPage returns a page for Korean", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"]).languages).not.toHaveProperty("ko");
  });

  it("includes Korean hreflang when a real *.ko.mdx page exists", async () => {
    existingDocs.add("agents.ko.mdx");
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "https://www.multica.ai/docs/agents",
      languages: {
        en: "https://www.multica.ai/docs/agents",
        zh: "https://www.multica.ai/docs/zh/agents",
        ko: "https://www.multica.ai/docs/ko/agents",
        "x-default": "https://www.multica.ai/docs/agents",
      },
    });
  });

  it("includes Japanese hreflang when a real *.ja.mdx page exists", async () => {
    existingDocs.add("agents.ja.mdx");
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "https://www.multica.ai/docs/agents",
      languages: {
        en: "https://www.multica.ai/docs/agents",
        zh: "https://www.multica.ai/docs/zh/agents",
        ja: "https://www.multica.ai/docs/ja/agents",
        "x-default": "https://www.multica.ai/docs/agents",
      },
    });
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

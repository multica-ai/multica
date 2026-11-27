// @vitest-environment node

import "next/dist/server/node-environment-baseline";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { NextRequest, type NextFetchEvent } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import nextConfig from "./next.config.mjs";
import { config, markdownRewritePath, proxy } from "./proxy";

describe("docs locale proxy matcher", () => {
  it.each([
    "/docs/icon.svg",
    "/docs/icon.svg?icon.1234.svg",
    "/docs/videos/demos/assign-light.mp4",
    "/docs/videos/demos/assign-light.jpg",
  ])(
    "serves static assets without a locale rewrite at %s",
    (url) => {
      expect(unstable_doesMiddlewareMatch({ config, nextConfig, url })).toBe(false);
    },
  );

  it.each([
    "/docs",
    "/docs/agents",
    "/docs/zh",
    "/docs/fr/agents",
    "/docs/llms.txt",
    "/docs/llms-full.txt",
    "/docs/agents.md",
  ])("keeps locale routing enabled for %s", (url) => {
    expect(unstable_doesMiddlewareMatch({ config, nextConfig, url })).toBe(true);
  });
});

describe("markdownRewritePath", () => {
  it.each([
    ["/agents.md", "/docs/en/llms.mdx/agents"],
    ["/agents.mdx", "/docs/en/llms.mdx/agents"],
    ["/developers/architecture.md", "/docs/en/llms.mdx/developers/architecture"],
    ["/index.md", "/docs/en/llms.mdx"],
    ["/zh/agents.md", "/docs/zh/llms.mdx/agents"],
    ["/zh-Hant/index.md", "/docs/zh-Hant/llms.mdx"],
  ])("serves %s as Markdown", (pathname, target) => {
    expect(markdownRewritePath(pathname, false)).toBe(target);
  });

  it.each([
    ["/", "/docs/en/llms.mdx"],
    ["/agents", "/docs/en/llms.mdx/agents"],
    ["/zh", "/docs/zh/llms.mdx"],
    ["/ko/agents", "/docs/ko/llms.mdx/agents"],
  ])("serves %s as Markdown when the client prefers it", (pathname, target) => {
    expect(markdownRewritePath(pathname, true)).toBe(target);
  });

  it("leaves HTML requests and files alone", () => {
    expect(markdownRewritePath("/agents", false)).toBeNull();
    expect(markdownRewritePath("/llms.txt", true)).toBeNull();
    expect(markdownRewritePath("/zh/llms-full.txt", true)).toBeNull();
  });
});

function request(path: string, headers: Record<string, string> = {}) {
  return new NextRequest(`https://www.multica.ai/docs${path}`, {
    headers,
    nextConfig: { basePath: "/docs" },
  });
}

const BROWSER = {
  "user-agent":
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
  accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
  "sec-fetch-mode": "navigate",
};

function rewriteTarget(response: Response): string | null {
  const target = response.headers.get("x-middleware-rewrite");
  return target ? new URL(target).pathname : null;
}

describe("docs proxy", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("serves the HTML page to browsers", () => {
    expect(rewriteTarget(proxy(request("/agents", BROWSER)))).toBe("/docs/en/agents");
  });

  it("serves the page Markdown for Accept: text/markdown", () => {
    const response = proxy(request("/agents", { accept: "text/markdown, text/html;q=0.9" }));
    expect(rewriteTarget(response)).toBe("/docs/en/llms.mdx/agents");
  });

  it("routes llms.txt through the default-language route", () => {
    expect(rewriteTarget(proxy(request("/llms.txt")))).toBe("/docs/en/llms.txt");
  });

  it("logs AI visits as one JSON line and stays quiet for browsers", () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    proxy(request("/agents", BROWSER));
    expect(info).not.toHaveBeenCalled();

    proxy(request("/agents.md", { "user-agent": "Claude-User/1.0", cookie: "session=secret" }));
    expect(info).toHaveBeenCalledTimes(1);
    const line = String(info.mock.calls[0]?.[0]);
    expect(JSON.parse(line)).toMatchObject({
      event: "ai_visit",
      app: "docs",
      path: "/docs/agents.md",
      agent: "Claude-User",
      format: "markdown",
      status: null,
    });
    expect(line).not.toContain("secret");
  });

  it("records the status of redirects the proxy answers itself", () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    proxy(request("/en/agents", { "user-agent": "GPTBot/1.1" }));
    expect(JSON.parse(String(info.mock.calls[0]?.[0]))).toMatchObject({ status: 307, format: "html" });
  });

  it("retains visits in PostHog after the response when a key is configured", () => {
    vi.spyOn(console, "info").mockImplementation(() => {});
    vi.stubEnv("POSTHOG_API_KEY", "phc_test");
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}"));
    const waitUntil = vi.fn();
    try {
      proxy(request("/agents.md", { "user-agent": "Claude-User/1.0" }), { waitUntil } as unknown as NextFetchEvent);
      proxy(request("/agents", BROWSER), { waitUntil } as unknown as NextFetchEvent);
    } finally {
      vi.unstubAllEnvs();
    }
    expect(waitUntil).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("docs cache keys", () => {
  it("declares Vary: Accept on page URLs, which answer HTML or Markdown", async () => {
    const rules = (await nextConfig.headers?.()) ?? [];
    const rule = rules.find((entry) => entry.headers.some((header) => header.key === "Vary" && header.value === "Accept"));
    expect(rule).toBeDefined();
    const matches = (path: string) => new RegExp(`^${rule!.source}$`).test(path);
    for (const page of ["/", "/agents", "/zh/agents", "/en/providers/claude-code"]) expect(matches(page)).toBe(true);
    for (const file of ["/agents.md", "/llms.txt", "/_next/static/chunk", "/api/search", "/icon.svg"]) expect(matches(file)).toBe(false);
  });
});

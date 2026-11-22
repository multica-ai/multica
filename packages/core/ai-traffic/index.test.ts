// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  aiVisitPostHogConfig,
  aiVisitPostHogEvent,
  buildAiVisitRecord,
  classifyAiTraffic,
  identifyAiAgent,
  logAiVisit,
  prefersMarkdown,
  reportAiVisit,
  sendAiVisitToPostHog,
} from "./index";

const CHROME_2026 =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36";
const CHROME_2017 =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.115 Safari/537.36";
const GOOGLEBOT_SMARTPHONE =
  "Mozilla/5.0 (Linux; Android 6.0.1; Nexus 5X Build/MMB29P) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Mobile Safari/537.36 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)";

function headers(values: Record<string, string>) {
  return new Headers(values);
}

const browserPageLoad = {
  "user-agent": CHROME_2026,
  accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
  "sec-fetch-mode": "navigate",
};

describe("identifyAiAgent", () => {
  it.each([
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", "ClaudeBot", "training"],
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Claude-User/1.0; +Claude-User@anthropic.com)", "Claude-User", "user"],
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; OAI-SearchBot/1.0; +https://openai.com/searchbot", "OAI-SearchBot", "search"],
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", "ChatGPT-User", "user"],
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; PerplexityBot/1.0; +https://perplexity.ai/perplexitybot)", "PerplexityBot", "search"],
    ["Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Perplexity-User/1.0; +https://perplexity.ai/perplexity-user)", "Perplexity-User", "user"],
    ["Mozilla/5.0 (Linux; Android 5.0) AppleWebKit/537.36 (KHTML, like Gecko) Mobile Safari/537.36 (compatible; Bytespider; spider-feedback@bytedance.com)", "Bytespider", "training"],
    ["Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.1 Safari/605.1.15 (Applebot/0.1; +http://www.apple.com/go/applebot)", "Applebot", "search"],
  ])("recognizes %s", (userAgent, name, kind) => {
    expect(identifyAiAgent(userAgent)).toMatchObject({ name, kind });
  });

  it("matches whole product tokens only", () => {
    expect(identifyAiAgent("NotClaudeBot/1.0")).toBeNull();
    expect(identifyAiAgent("ClaudeBotanist/1.0")).toBeNull();
    expect(identifyAiAgent(CHROME_2026)).toBeNull();
    expect(identifyAiAgent(null)).toBeNull();
  });
});

describe("prefersMarkdown", () => {
  it.each([
    "text/markdown",
    "text/markdown, text/html;q=0.9, */*;q=0.8",
    "text/x-markdown",
  ])("prefers Markdown for %s", (accept) => {
    expect(prefersMarkdown(accept)).toBe(true);
  });

  it.each([
    null,
    "",
    "*/*",
    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "text/html, text/markdown;q=0.5",
    "text/markdown;q=0",
    "text/plain",
    "application/json, text/plain, */*",
  ])("keeps HTML for %s", (accept) => {
    expect(prefersMarkdown(accept)).toBe(false);
  });
});

describe("classifyAiTraffic", () => {
  it("ignores an ordinary browser page load", () => {
    expect(classifyAiTraffic(headers(browserPageLoad))).toBeNull();
    expect(classifyAiTraffic(headers({ "user-agent": CHROME_2026, cookie: "a=1" }))).toBeNull();
  });

  it("names the agent and whether it asked for Markdown", () => {
    expect(
      classifyAiTraffic(headers({ "user-agent": "Claude-User/1.0", accept: "text/markdown" })),
    ).toMatchObject({ signal: "ai-agent", agent: { name: "Claude-User" }, markdownRequested: true });
  });

  it("flags an unnamed client that asks for Markdown", () => {
    expect(classifyAiTraffic(headers({ "user-agent": "node", accept: "text/markdown" }))).toMatchObject({
      signal: "markdown-request",
      agent: null,
    });
  });

  it("flags a browser user agent without cookies or fetch metadata", () => {
    expect(classifyAiTraffic(headers({ "user-agent": CHROME_2017 }))).toEqual({
      signal: "disguised-browser",
      agent: null,
      markdownRequested: false,
      legacyBrowser: true,
    });
    expect(classifyAiTraffic(headers({ "user-agent": CHROME_2026 }))).toMatchObject({
      signal: "disguised-browser",
      legacyBrowser: false,
    });
  });

  it("does not call declared crawlers or non-browser clients disguised", () => {
    expect(classifyAiTraffic(headers({ "user-agent": GOOGLEBOT_SMARTPHONE }))).toBeNull();
    expect(classifyAiTraffic(headers({ "user-agent": "curl/8.7.1" }))).toBeNull();
  });
});

describe("buildAiVisitRecord", () => {
  it("records the visit without cookies, tokens, addresses or query strings", () => {
    const record = buildAiVisitRecord({
      app: "docs",
      method: "GET",
      pathname: "/docs/agents",
      headers: headers({
        "user-agent": "ChatGPT-User/1.0",
        cookie: "multica_session=secret",
        authorization: "Bearer secret",
        "x-forwarded-for": "203.0.113.7",
      }),
      format: "html",
      status: null,
      now: new Date("2026-11-23T00:00:00Z"),
    });

    expect(record).toEqual({
      event: "ai_visit",
      time: "2026-11-23T00:00:00.000Z",
      app: "docs",
      method: "GET",
      path: "/docs/agents",
      signal: "ai-agent",
      agent: "ChatGPT-User",
      operator: "OpenAI",
      kind: "user",
      format: "html",
      status: null,
      legacyBrowser: false,
      userAgent: "ChatGPT-User/1.0",
    });
    const serialized = JSON.stringify(record);
    expect(serialized).not.toContain("secret");
    expect(serialized).not.toContain("203.0.113.7");
  });

  it("records any client fetching a Markdown URL", () => {
    expect(
      buildAiVisitRecord({
        app: "docs",
        method: "GET",
        pathname: "/docs/agents.md",
        headers: headers({ "user-agent": "curl/8.7.1", accept: "*/*" }),
        format: "markdown",
        status: null,
      }),
    ).toMatchObject({ signal: "markdown-request", agent: null, format: "markdown" });
  });

  it("skips ordinary traffic", () => {
    expect(
      buildAiVisitRecord({
        app: "web",
        method: "GET",
        pathname: "/",
        headers: headers(browserPageLoad),
        format: "html",
        status: null,
      }),
    ).toBeNull();
  });

  it("bounds the path and user agent", () => {
    const record = buildAiVisitRecord({
      app: "web",
      method: "GET",
      pathname: `/${"a".repeat(2000)}`,
      headers: headers({ "user-agent": `GPTBot/1.0 ${"x".repeat(2000)}` }),
      format: "html",
      status: null,
    });
    expect(record?.path).toHaveLength(512);
    expect(record?.userAgent).toHaveLength(256);
  });
});

describe("logAiVisit", () => {
  it("writes one JSON line per record and nothing for ordinary traffic", () => {
    const lines: string[] = [];
    const record = buildAiVisitRecord({
      app: "web",
      method: "GET",
      pathname: "/llms.txt",
      headers: headers({ "user-agent": "PerplexityBot/1.0" }),
      format: "markdown",
      status: null,
    });
    logAiVisit(record, (line) => lines.push(line));
    logAiVisit(null, (line) => lines.push(line));
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0]!)).toMatchObject({ agent: "PerplexityBot", path: "/llms.txt" });
  });

  it("swallows sink failures", () => {
    const record = buildAiVisitRecord({
      app: "web",
      method: "GET",
      pathname: "/",
      headers: headers({ "user-agent": "GPTBot/1.0" }),
      format: "html",
      status: null,
    });
    expect(() =>
      logAiVisit(record, () => {
        throw new Error("stdout closed");
      }),
    ).not.toThrow();
  });
});

describe("PostHog retention", () => {
  const record = buildAiVisitRecord({
    app: "docs",
    method: "GET",
    pathname: "/docs/agents.md",
    headers: headers({ "user-agent": "ClaudeBot/1.0", cookie: "session=secret" }),
    format: "markdown",
    status: null,
    now: new Date("2026-11-23T00:00:00.000Z"),
  })!;
  const config = { apiKey: "phc_test", host: "https://eu.i.posthog.com", environment: "production" } as const;

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("reads the same variables as the Go server", () => {
    expect(aiVisitPostHogConfig({})).toBeNull();
    expect(aiVisitPostHogConfig({ POSTHOG_API_KEY: "phc_test", ANALYTICS_DISABLED: "TRUE" })).toBeNull();
    expect(aiVisitPostHogConfig({ POSTHOG_API_KEY: " phc_test " })).toEqual({
      apiKey: "phc_test",
      host: "https://us.i.posthog.com",
      environment: "dev",
    });
    expect(
      aiVisitPostHogConfig({ POSTHOG_API_KEY: "phc_test", POSTHOG_HOST: "https://eu.i.posthog.com/", APP_ENV: "prod" }),
    ).toEqual(config);
    expect(
      aiVisitPostHogConfig({ POSTHOG_API_KEY: "phc_test", ANALYTICS_ENVIRONMENT: "staging", APP_ENV: "production" })
        ?.environment,
    ).toBe("staging");
  });

  it("captures bots without person profiles or GeoIP", () => {
    expect(aiVisitPostHogEvent(record, "production")).toEqual({
      event: "ai_visit",
      distinct_id: "ai-visit:ClaudeBot",
      timestamp: "2026-11-23T00:00:00.000Z",
      properties: {
        app: "docs",
        method: "GET",
        path: "/docs/agents.md",
        signal: "ai-agent",
        agent: "ClaudeBot",
        operator: "Anthropic",
        kind: "training",
        format: "markdown",
        status: null,
        legacyBrowser: false,
        userAgent: "ClaudeBot/1.0",
        environment: "production",
        $process_person_profile: false,
        $geoip_disable: true,
      },
    });
  });

  it("posts to the batch endpoint and never rejects", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}"));
    await sendAiVisitToPostHog(record, config, fetchMock);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://eu.i.posthog.com/batch/");
    const body = JSON.parse(String(init?.body));
    expect(body.api_key).toBe("phc_test");
    expect(body.batch).toEqual([aiVisitPostHogEvent(record, "production")]);
    expect(String(init?.body)).not.toContain("secret");

    await expect(
      sendAiVisitToPostHog(record, config, vi.fn<typeof fetch>().mockRejectedValue(new Error("offline"))),
    ).resolves.toBeUndefined();
  });

  it("hands the send to waitUntil only when PostHog is configured", () => {
    vi.spyOn(console, "info").mockImplementation(() => {});
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response("{}"));
    const waitUntil = vi.fn();

    reportAiVisit(record, { env: {}, waitUntil, fetch: fetchMock });
    reportAiVisit(null, { env: { POSTHOG_API_KEY: "phc_test" }, waitUntil, fetch: fetchMock });
    expect(waitUntil).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();

    reportAiVisit(record, { env: { POSTHOG_API_KEY: "phc_test" }, waitUntil, fetch: fetchMock });
    expect(waitUntil).toHaveBeenCalledWith(expect.any(Promise));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(console.info).toHaveBeenCalledTimes(2);
  });
});

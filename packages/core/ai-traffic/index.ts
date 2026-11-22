/**
 * Server-side classification of AI crawler and AI agent traffic on the public
 * site and docs. Client analytics never see these requests: crawlers and
 * real-time fetchers do not execute JavaScript.
 */

export type AiAgentKind =
  /** Bulk crawler that collects training data. */
  | "training"
  /** Crawler that builds an AI search index. */
  | "search"
  /** Fetch made on behalf of a person, during a chat or an agent run. */
  | "user";

export type AiAgent = {
  /** Product token as it appears in the user agent. */
  name: string;
  operator: string;
  kind: AiAgentKind;
};

// Only tokens operators send as user agents. Robots-only tokens such as
// `Google-Extended` and `Applebot-Extended` never appear on a request; their
// traffic arrives as Googlebot or Applebot.
export const AI_AGENTS: readonly AiAgent[] = [
  { name: "ClaudeBot", operator: "Anthropic", kind: "training" },
  { name: "Claude-SearchBot", operator: "Anthropic", kind: "search" },
  { name: "Claude-User", operator: "Anthropic", kind: "user" },
  { name: "GPTBot", operator: "OpenAI", kind: "training" },
  { name: "OAI-SearchBot", operator: "OpenAI", kind: "search" },
  { name: "ChatGPT-User", operator: "OpenAI", kind: "user" },
  { name: "PerplexityBot", operator: "Perplexity", kind: "search" },
  { name: "Perplexity-User", operator: "Perplexity", kind: "user" },
  { name: "Bytespider", operator: "ByteDance", kind: "training" },
  { name: "Applebot", operator: "Apple", kind: "search" },
  { name: "meta-externalagent", operator: "Meta", kind: "training" },
  { name: "Meta-ExternalFetcher", operator: "Meta", kind: "user" },
  { name: "Amazonbot", operator: "Amazon", kind: "search" },
  { name: "DuckAssistBot", operator: "DuckDuckGo", kind: "search" },
  { name: "MistralAI-User", operator: "Mistral", kind: "user" },
  { name: "CCBot", operator: "Common Crawl", kind: "training" },
];

const AI_AGENT_PATTERN = new RegExp(
  `(?:^|[^a-z0-9-])(${AI_AGENTS.map((agent) => agent.name.replace(/[.*+?^${}()|[\]\\-]/g, "\\$&")).join("|")})(?![a-z0-9-])`,
  "i",
);

export function identifyAiAgent(userAgent: string | null | undefined): AiAgent | null {
  if (!userAgent) return null;
  const token = AI_AGENT_PATTERN.exec(userAgent)?.[1]?.toLowerCase();
  if (!token) return null;
  return AI_AGENTS.find((agent) => agent.name.toLowerCase() === token) ?? null;
}

// Not `text/plain`: generic HTTP clients (axios sends
// `application/json, text/plain, */*`) list it without wanting Markdown.
const MARKDOWN_MEDIA_TYPES = new Set(["text/markdown", "text/x-markdown"]);
const HTML_MEDIA_TYPES = new Set(["text/html", "text/*", "*/*"]);

/**
 * Whether an `Accept` header ranks Markdown at least as high as HTML.
 * Browsers list `text/html` explicitly and never `text/markdown`, so they
 * keep getting HTML.
 */
export function prefersMarkdown(accept: string | null | undefined): boolean {
  if (!accept) return false;
  let markdown = 0;
  let html = 0;
  for (const section of accept.split(",")) {
    const [rawType, ...params] = section.split(";");
    const mediaType = rawType?.trim().toLowerCase();
    if (!mediaType) continue;
    let quality = 1;
    for (const param of params) {
      const [key, value] = param.split("=");
      if (key?.trim().toLowerCase() !== "q") continue;
      const parsed = Number.parseFloat(value ?? "");
      if (!Number.isNaN(parsed)) quality = parsed;
    }
    if (quality <= 0) continue;
    if (MARKDOWN_MEDIA_TYPES.has(mediaType)) markdown = Math.max(markdown, quality);
    else if (HTML_MEDIA_TYPES.has(mediaType)) html = Math.max(html, quality);
  }
  return markdown > 0 && markdown >= html;
}

/** Chrome releases before this major predate 2022. */
const LEGACY_CHROME_MAJOR = 100;

function chromeMajor(userAgent: string): number | null {
  const match = /(?:Chrome|CriOS)\/(\d+)\./.exec(userAgent);
  return match ? Number(match[1]) : null;
}

/** Claims to be a browser and does not declare itself a bot, as Googlebot's browser-like UA does. */
function isBrowserUserAgent(userAgent: string): boolean {
  return (
    /^Mozilla\/5\.0 /.test(userAgent) &&
    /(?:Chrome|CriOS|Firefox|Safari)\//.test(userAgent) &&
    !/bot\b|crawl|spider|compatible;/i.test(userAgent)
  );
}

type RequestHeaders = { get(name: string): string | null };

export type AiTrafficSignal =
  /** The user agent names a known AI crawler or fetcher. */
  | "ai-agent"
  /** No known agent, but the client asked for Markdown, by `Accept` header or by URL. */
  | "markdown-request"
  /**
   * A browser user agent with none of a browser's request traits: no cookies
   * and no `Sec-Fetch-*` headers, which every current browser sends on page
   * loads. AI fetchers that hide behind browser user agents look like this.
   */
  | "disguised-browser";

export type AiTrafficClassification = {
  signal: AiTrafficSignal;
  agent: AiAgent | null;
  markdownRequested: boolean;
  /** The user agent claims a Chrome major older than 2022. */
  legacyBrowser: boolean;
};

export function classifyAiTraffic(headers: RequestHeaders): AiTrafficClassification | null {
  const userAgent = headers.get("user-agent") ?? "";
  const markdownRequested = prefersMarkdown(headers.get("accept"));
  const major = chromeMajor(userAgent);
  const legacyBrowser = major !== null && major < LEGACY_CHROME_MAJOR;

  const agent = identifyAiAgent(userAgent);
  if (agent) return { signal: "ai-agent", agent, markdownRequested, legacyBrowser };
  if (markdownRequested) return { signal: "markdown-request", agent: null, markdownRequested, legacyBrowser };
  if (
    isBrowserUserAgent(userAgent) &&
    !headers.get("cookie") &&
    !headers.get("sec-fetch-mode")
  ) {
    return { signal: "disguised-browser", agent: null, markdownRequested, legacyBrowser };
  }
  return null;
}

export type AiVisitFormat = "html" | "markdown" | "text";

export type AiVisitRecord = {
  event: "ai_visit";
  time: string;
  app: string;
  method: string;
  /** Pathname only; query strings can carry tokens. */
  path: string;
  signal: AiTrafficSignal;
  agent: string | null;
  operator: string | null;
  kind: AiAgentKind | null;
  format: AiVisitFormat;
  /** Status when the proxy answers itself (redirects); null when the route renders it. */
  status: number | null;
  legacyBrowser: boolean;
  /** Truncated user agent, kept to audit the classification. */
  userAgent: string;
};

const MAX_PATH_LENGTH = 512;
const MAX_USER_AGENT_LENGTH = 256;

/**
 * One log record per AI visit, or null for ordinary traffic. Never includes
 * cookies, authorization headers, IP addresses, or query strings.
 */
export function buildAiVisitRecord(input: {
  app: string;
  method: string;
  pathname: string;
  headers: RequestHeaders;
  format: AiVisitFormat;
  status: number | null;
  now?: Date;
}): AiVisitRecord | null {
  const traffic =
    classifyAiTraffic(input.headers) ??
    (input.format === "markdown"
      ? { signal: "markdown-request" as const, agent: null, legacyBrowser: false }
      : null);
  if (!traffic) return null;
  return {
    event: "ai_visit",
    time: (input.now ?? new Date()).toISOString(),
    app: input.app,
    method: input.method,
    path: input.pathname.slice(0, MAX_PATH_LENGTH),
    signal: traffic.signal,
    agent: traffic.agent?.name ?? null,
    operator: traffic.agent?.operator ?? null,
    kind: traffic.agent?.kind ?? null,
    format: input.format,
    status: input.status,
    legacyBrowser: traffic.legacyBrowser,
    userAgent: (input.headers.get("user-agent") ?? "").slice(0, MAX_USER_AGENT_LENGTH),
  };
}

/** Writes one JSON line to stdout. Never throws into the request path. */
export function logAiVisit(record: AiVisitRecord | null, sink: (line: string) => void = console.info): void {
  if (!record) return;
  try {
    sink(JSON.stringify(record));
  } catch {
    // Observability must not affect the response.
  }
}

export type AiVisitPostHogConfig = {
  apiKey: string;
  host: string;
  environment: "production" | "staging" | "dev";
};

type Env = Readonly<Record<string, string | undefined>>;

function analyticsEnvironment(value: string | undefined): AiVisitPostHogConfig["environment"] | null {
  switch (value?.trim().toLowerCase()) {
    case "production":
    case "prod":
      return "production";
    case "staging":
    case "stage":
      return "staging";
    case "development":
    case "dev":
    case "test":
    case "local":
      return "dev";
    default:
      return null;
  }
}

/**
 * PostHog settings from the same variables the Go server reads
 * (`server/internal/analytics`): no `POSTHOG_API_KEY`, or
 * `ANALYTICS_DISABLED`, means nothing is sent.
 */
export function aiVisitPostHogConfig(env: Env): AiVisitPostHogConfig | null {
  const apiKey = env.POSTHOG_API_KEY?.trim();
  const disabled = env.ANALYTICS_DISABLED?.trim().toLowerCase();
  if (!apiKey || disabled === "1" || disabled === "true") return null;
  return {
    apiKey,
    host: (env.POSTHOG_HOST?.trim() || "https://us.i.posthog.com").replace(/\/+$/, ""),
    environment: analyticsEnvironment(env.ANALYTICS_ENVIRONMENT) ?? analyticsEnvironment(env.APP_ENV) ?? "dev",
  };
}

/**
 * The PostHog capture for one visit. Bots are not people: one distinct id per
 * agent (or signal), no person profile, and no GeoIP, since the request
 * reaching PostHog comes from our server rather than the visitor.
 */
export function aiVisitPostHogEvent(record: AiVisitRecord, environment: AiVisitPostHogConfig["environment"]) {
  const { event, time, ...properties } = record;
  return {
    event,
    distinct_id: `ai-visit:${record.agent ?? record.signal}`,
    timestamp: time,
    properties: {
      ...properties,
      environment,
      $process_person_profile: false,
      $geoip_disable: true,
    },
  };
}

const POSTHOG_TIMEOUT_MS = 3000;

/** Sends one visit to PostHog's batch endpoint. Resolves on failure too. */
export async function sendAiVisitToPostHog(
  record: AiVisitRecord,
  config: AiVisitPostHogConfig,
  fetchImpl: typeof fetch = fetch,
): Promise<void> {
  try {
    const response = await fetchImpl(`${config.host}/batch/`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ api_key: config.apiKey, batch: [aiVisitPostHogEvent(record, config.environment)] }),
      signal: AbortSignal.timeout(POSTHOG_TIMEOUT_MS),
    });
    // Unread bodies hold their pooled connection open.
    await response.body?.cancel();
  } catch {
    // Observability must not affect the response.
  }
}

/**
 * Logs the visit and, when PostHog is configured, sends it for retention.
 * The send runs after the response through `waitUntil`, so it never adds
 * latency to the request.
 */
export function reportAiVisit(
  record: AiVisitRecord | null,
  options: { env: Env; waitUntil?: (promise: Promise<unknown>) => void; fetch?: typeof fetch },
): void {
  if (!record) return;
  logAiVisit(record);
  const config = aiVisitPostHogConfig(options.env);
  if (!config) return;
  const sent = sendAiVisitToPostHog(record, config, options.fetch);
  try {
    options.waitUntil?.(sent);
  } catch {
    // Observability must not affect the response.
  }
}

export type HeaderReader = {
  get(name: string): string | null;
};

export type SearchParams = Record<string, string | string[] | undefined>;

const MOBILE_USER_AGENT_PATTERN =
  /\b(Android|BlackBerry|iPhone|iPad|iPod|IEMobile|Mobi|Mobile Safari|Opera Mini|Windows Phone|webOS)\b/i;
const WECHAT_USER_AGENT_PATTERN = /\bMicroMessenger\b/i;

export function isMobileUserAgent(userAgent?: string | null): boolean {
  if (!userAgent) return false;
  return MOBILE_USER_AGENT_PATTERN.test(userAgent);
}

export function isWeChatUserAgent(userAgent?: string | null): boolean {
  if (!userAgent) return false;
  return WECHAT_USER_AGENT_PATTERN.test(userAgent);
}

export function buildIssueWebHref({
  headers,
  workspaceSlug,
  issueId,
  searchParams,
}: {
  headers: HeaderReader;
  workspaceSlug: string;
  issueId: string;
  searchParams?: SearchParams;
}): string {
  const origin = getRequestOrigin(headers);
  const path = `/${encodeURIComponent(workspaceSlug)}/issues/${encodeURIComponent(issueId)}`;
  const queryString = buildQueryString(searchParams);

  return `${origin}${path}${queryString ? `?${queryString}` : ""}`;
}

export function buildIssueMobileAppHref({
  workspaceSlug,
  issueId,
  searchParams,
}: {
  workspaceSlug: string;
  issueId: string;
  searchParams?: SearchParams;
}): string {
  const path = `${encodeURIComponent(workspaceSlug)}/issues/${encodeURIComponent(issueId)}`;
  const queryString = buildQueryString(searchParams);

  return `wujieai-multicam://${path}${queryString ? `?${queryString}` : ""}`;
}

function getRequestOrigin(headers: HeaderReader): string {
  const host =
    firstHeaderValue(headers.get("x-forwarded-host")) ||
    firstHeaderValue(headers.get("host")) ||
    "multica.wujieai.com";
  const proto =
    firstHeaderValue(headers.get("x-forwarded-proto")) ||
    (isLocalHost(host) ? "http" : "https");

  return `${proto}://${host}`;
}

function buildQueryString(searchParams?: SearchParams): string {
  if (!searchParams) return "";

  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (typeof value === "string") {
      query.append(key, value);
      continue;
    }

    for (const item of value ?? []) {
      query.append(key, item);
    }
  }

  return query.toString();
}

function firstHeaderValue(value: string | null): string | null {
  return value?.split(",")[0]?.trim() || null;
}

function isLocalHost(host: string): boolean {
  return /^(localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$/i.test(host);
}

const START_PAGE_COOKIE_DESKTOP = "start_page_desktop";
const START_PAGE_COOKIE_MOBILE = "start_page_mobile";

const VALID_START_PAGES = new Set([
  "issues",
  "inbox",
  "my-issues",
  "projects",
  "documents",
  "notifications",
]);

type CookieReader = {
  get(name: string): { value?: string } | undefined;
};

function validStartPage(value: string | undefined): string {
  return value && VALID_START_PAGES.has(value) ? value : "issues";
}

function isMobileViewport(headers: Headers): boolean {
  const viewportWidth = headers.get("sec-ch-viewport-width");
  if (viewportWidth) {
    const width = Number.parseInt(viewportWidth, 10);
    if (Number.isFinite(width)) return width < 768;
  }

  const mobileHint = headers.get("sec-ch-ua-mobile");
  if (mobileHint) return mobileHint.includes("?1");

  const userAgent = headers.get("user-agent") ?? "";
  return /\b(Android|iPhone|iPod|Mobile)\b/i.test(userAgent);
}

export function resolveStartPageFromCookies(
  cookies: CookieReader,
  headers: Headers,
): string {
  const key = isMobileViewport(headers)
    ? START_PAGE_COOKIE_MOBILE
    : START_PAGE_COOKIE_DESKTOP;
  return validStartPage(cookies.get(key)?.value);
}

export function startPagePath(
  workspaceSlug: string,
  cookies: CookieReader,
  headers: Headers,
): string {
  const page = resolveStartPageFromCookies(cookies, headers);
  return `/${encodeURIComponent(workspaceSlug)}/${page}`;
}

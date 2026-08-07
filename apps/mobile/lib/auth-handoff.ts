const AUTH_CALLBACK_PROTOCOL = "multica:";
const AUTH_CALLBACK_HOST = "auth";
const AUTH_CALLBACK_PATH = "/callback";
export const AUTH_CALLBACK_URL = "multica://auth/callback";

export function buildMobileLoginUrl(webUrl: string): string | null {
  try {
    const url = new URL("/login", webUrl);
    if (url.protocol !== "https:" && url.protocol !== "http:") return null;
    url.searchParams.set("platform", "mobile");
    return url.toString();
  } catch {
    return null;
  }
}

export function getAuthHandoffToken(url: string | null): string | null {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    if (
      parsed.protocol !== AUTH_CALLBACK_PROTOCOL ||
      parsed.hostname !== AUTH_CALLBACK_HOST ||
      parsed.pathname !== AUTH_CALLBACK_PATH
    ) {
      return null;
    }
    return parsed.searchParams.get("token")?.trim() || null;
  } catch {
    return null;
  }
}

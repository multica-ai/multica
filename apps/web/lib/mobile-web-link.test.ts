import { describe, expect, it } from "vitest";
import {
  buildIssueMobileAppHref,
  buildIssueWebHref,
  isMobileUserAgent,
  isWeChatUserAgent,
  type HeaderReader,
} from "./mobile-web-link";

function headers(values: Record<string, string>): HeaderReader {
  return {
    get: (name) => values[name.toLowerCase()] ?? null,
  };
}

describe("mobile web links", () => {
  it("detects mobile user agents", () => {
    expect(isMobileUserAgent(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148",
    )).toBe(true);
    expect(isMobileUserAgent(
      "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/125.0 Mobile Safari/537.36",
    )).toBe(true);
    expect(isMobileUserAgent(
      "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 MicroMessenger",
    )).toBe(true);
  });

  it("rejects desktop and empty user agents", () => {
    expect(isMobileUserAgent(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/125.0 Safari/537.36",
    )).toBe(false);
    expect(isMobileUserAgent(
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125.0 Safari/537.36",
    )).toBe(false);
    expect(isMobileUserAgent("")).toBe(false);
    expect(isMobileUserAgent(null)).toBe(false);
  });

  it("detects WeChat embedded browser user agents", () => {
    expect(isWeChatUserAgent(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 MicroMessenger/8.0.49",
    )).toBe(true);
    expect(isWeChatUserAgent(
      "Mozilla/5.0 (Linux; Android 15; 24018RPACC) AppleWebKit/537.36 Mobile Safari/537.36 MicroMessenger/8.0.50",
    )).toBe(true);
    expect(isWeChatUserAgent(
      "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/125.0 Mobile Safari/537.36",
    )).toBe(false);
    expect(isWeChatUserAgent(null)).toBe(false);
  });

  it("builds an absolute issue href with forwarded production headers", () => {
    expect(buildIssueWebHref({
      headers: headers({
        "x-forwarded-host": "multica.wujieai.com",
        "x-forwarded-proto": "https",
      }),
      workspaceSlug: "openharness",
      issueId: "OPE-2151",
      searchParams: { comment: "9441bec9-0a96-40dc-ab14-0547423fdb4f" },
    })).toBe(
      "https://multica.wujieai.com/openharness/issues/OPE-2151?comment=9441bec9-0a96-40dc-ab14-0547423fdb4f",
    );
  });

  it("keeps repeated query params and uses http for local dev hosts", () => {
    expect(buildIssueWebHref({
      headers: headers({ host: "127.0.0.1:3000" }),
      workspaceSlug: "test",
      issueId: "TES-1",
      searchParams: { label: ["bug", "mobile"], empty: undefined },
    })).toBe("http://127.0.0.1:3000/test/issues/TES-1?label=bug&label=mobile");
  });

  it("builds a mobile app issue href for the Web fallback banner", () => {
    expect(buildIssueMobileAppHref({
      workspaceSlug: "openharness",
      issueId: "OPE-2151",
      searchParams: { comment: "9441bec9-0a96-40dc-ab14-0547423fdb4f" },
    })).toBe(
      "wujieai-multicam://openharness/issues/OPE-2151?comment=9441bec9-0a96-40dc-ab14-0547423fdb4f",
    );
  });
});

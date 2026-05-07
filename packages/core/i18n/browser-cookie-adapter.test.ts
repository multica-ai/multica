// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  LOCALE_COOKIE,
  createBrowserCookieLocaleAdapter,
} from "./browser-cookie-adapter";

function clearCookies() {
  document.cookie
    .split(";")
    .map((c) => c.trim().split("=")[0])
    .filter(Boolean)
    .forEach((name) => {
      document.cookie = `${name}=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT`;
    });
}

describe("createBrowserCookieLocaleAdapter", () => {
  beforeEach(clearCookies);
  afterEach(clearCookies);

  it("getUserChoice returns null when no cookie is set", () => {
    const adapter = createBrowserCookieLocaleAdapter();
    expect(adapter.getUserChoice()).toBe(null);
  });

  it("getUserChoice round-trips a persisted value", () => {
    const adapter = createBrowserCookieLocaleAdapter();
    adapter.persist("zh-Hans");
    expect(adapter.getUserChoice()).toBe("zh-Hans");
  });

  it("getUserChoice decodes URI-encoded cookie values", () => {
    document.cookie = `${LOCALE_COOKIE}=${encodeURIComponent("zh-Hans")}; path=/`;
    expect(createBrowserCookieLocaleAdapter().getUserChoice()).toBe("zh-Hans");
  });

  it("getUserChoice ignores unrelated cookies that share a prefix", () => {
    document.cookie = `${LOCALE_COOKIE}-other=should-not-match; path=/`;
    expect(createBrowserCookieLocaleAdapter().getUserChoice()).toBe(null);
  });

  it("persist writes a cookie with SameSite=Lax", () => {
    const adapter = createBrowserCookieLocaleAdapter();
    adapter.persist("en");
    expect(document.cookie).toContain(`${LOCALE_COOKIE}=en`);
  });

  it("getSystemPreferences mirrors navigator.languages", () => {
    Object.defineProperty(navigator, "languages", {
      value: ["zh-Hans-CN", "en-US"],
      configurable: true,
    });
    expect(createBrowserCookieLocaleAdapter().getSystemPreferences()).toEqual([
      "zh-Hans-CN",
      "en-US",
    ]);
  });
});

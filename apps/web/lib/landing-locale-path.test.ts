import { readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { isReservedSlug } from "@multica/core/paths";
import {
  LANDING_PATH_PREFIXES,
  LANDING_ROUTES,
  landingAlternates,
  landingPathLocale,
  localizeHref,
  splitLandingLocale,
  stripLandingLocale,
} from "./landing-locale-path";

describe("landing locale paths", () => {
  it("lists every route under app/(landing)", () => {
    const dir = join(__dirname, "../app/(landing)");
    const routes = readdirSync(dir, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
    expect([...LANDING_ROUTES].sort()).toEqual(routes.sort());
  });

  it("reserves every prefix as a workspace slug", () => {
    for (const prefix of Object.keys(LANDING_PATH_PREFIXES)) expect(isReservedSlug(prefix)).toBe(true);
  });

  it("splits prefixed marketing URLs", () => {
    expect(splitLandingLocale("/zh")).toEqual({ locale: "zh-Hans", path: "/" });
    expect(splitLandingLocale("/ko/download")).toEqual({ locale: "ko", path: "/download" });
    expect(splitLandingLocale("/ja/usecases/auto-data-analysis")).toEqual({
      locale: "ja",
      path: "/usecases/auto-data-analysis",
    });
  });

  it("leaves app and unprefixed URLs alone", () => {
    expect(splitLandingLocale("/download")).toBeNull();
    expect(splitLandingLocale("/zh/issues")).toBeNull();
    expect(splitLandingLocale("/zhong/download")).toBeNull();
    expect(stripLandingLocale("/acme/issues")).toBe("/acme/issues");
    expect(stripLandingLocale("/zh/about")).toBe("/about");
  });

  it("maps locales onto the languages the site has", () => {
    expect(landingPathLocale("zh-Hant")).toBe("zh-Hans");
    expect(landingPathLocale("fr")).toBe("en");
    expect(landingPathLocale("ja")).toBe("ja");
  });

  it("prefixes marketing links only", () => {
    expect(localizeHref("zh-Hans", "/")).toBe("/zh");
    expect(localizeHref("zh-Hans", "/download?started=mac#top")).toBe("/zh/download?started=mac#top");
    expect(localizeHref("ko", "/usecases/x")).toBe("/ko/usecases/x");
    expect(localizeHref("ja", "/login")).toBe("/login");
    expect(localizeHref("ja", "/docs/ja")).toBe("/docs/ja");
    expect(localizeHref("ja", "https://github.com/multica-ai/multica")).toBe("https://github.com/multica-ai/multica");
    expect(localizeHref("ja", "/zh/about")).toBe("/zh/about");
    expect(localizeHref("en", "/about")).toBe("/about");
    expect(localizeHref("fr", "/about")).toBe("/about");
  });

  it("lists every language's URL with English as the default", () => {
    expect(landingAlternates("/", "https://www.multica.ai")).toEqual({
      en: "https://www.multica.ai/",
      "zh-Hans": "https://www.multica.ai/zh",
      ko: "https://www.multica.ai/ko",
      ja: "https://www.multica.ai/ja",
      "x-default": "https://www.multica.ai/",
    });
    expect(landingAlternates("/download")).toMatchObject({ "zh-Hans": "/zh/download", "x-default": "/download" });
  });
});

import { describe, expect, it } from "vitest";
import { isNavigationRequest, LEGACY_NAVIGATION_CACHES } from "./sw-navigation";

const ORIGIN = "https://sara.tailbde0.ts.net";

interface MatchOpts {
  url: string;
  init?: RequestInit;
  sameOrigin?: boolean;
  mode?: RequestMode;
  destination?: RequestDestination;
}

function match(opts: MatchOpts): boolean {
  const url = new URL(opts.url);
  const request = new Request(opts.url, opts.init);
  // `mode` and `destination` are restricted-write on Request (the browser sets
  // them based on the originating element); for tests we override via
  // defineProperty so we can simulate every browser-issued case.
  if (opts.mode !== undefined) {
    Object.defineProperty(request, "mode", { value: opts.mode });
  }
  if (opts.destination !== undefined) {
    Object.defineProperty(request, "destination", { value: opts.destination });
  }
  return isNavigationRequest({
    request,
    url,
    sameOrigin: opts.sameOrigin ?? url.origin === ORIGIN,
  });
}

describe("isNavigationRequest — page documents", () => {
  it("matches top-level browser navigations (mode=navigate)", () => {
    expect(match({ url: `${ORIGIN}/login`, mode: "navigate" })).toBe(true);
  });

  it("matches navigations with query strings (e.g. /login?platform=desktop)", () => {
    expect(
      match({ url: `${ORIGIN}/login?platform=desktop`, mode: "navigate" }),
    ).toBe(true);
  });

  it("matches workspace-scoped pages (/jeh-*/issues)", () => {
    expect(match({ url: `${ORIGIN}/jeh-b0edd870/issues`, mode: "navigate" })).toBe(
      true,
    );
  });

  it("matches deep page paths (/jeh-*/issues/JEH-1832)", () => {
    expect(
      match({ url: `${ORIGIN}/jeh-b0edd870/issues/JEH-1832`, mode: "navigate" }),
    ).toBe(true);
  });

  it("matches the root path", () => {
    expect(match({ url: `${ORIGIN}/`, mode: "navigate" })).toBe(true);
  });

  it("matches document-destination fetches even when mode is not 'navigate'", () => {
    expect(match({ url: `${ORIGIN}/login`, destination: "document" })).toBe(
      true,
    );
  });

  it("matches `<link rel=\"prefetch\">`-style fetches (mode=no-cors, empty destination, no Accept)", () => {
    // Concrete repro: Chrome issues these for speculation-rule prefetches and
    // anchor prefetches. They have no header signals at all. The response is
    // text/html and must NOT end up in `others`.
    expect(
      match({
        url: `${ORIGIN}/login`,
        mode: "no-cors",
        destination: "",
      }),
    ).toBe(true);
  });

  it("matches fetches with Accept: text/html (older client-side prefetch path)", () => {
    expect(
      match({
        url: `${ORIGIN}/jeh-b0edd870/issues/JEH-1832`,
        init: { headers: { accept: "text/html,application/xhtml+xml" } },
      }),
    ).toBe(true);
  });
});

describe("isNavigationRequest — exclusions", () => {
  it("does not match cross-origin requests", () => {
    expect(
      match({ url: "https://fonts.gstatic.com/x.woff2", sameOrigin: false }),
    ).toBe(false);
  });

  it("does not match /api/* requests (defaultCache routes those to apis cache)", () => {
    expect(
      match({
        url: `${ORIGIN}/api/issues`,
        init: { headers: { accept: "text/html" } },
      }),
    ).toBe(false);
  });

  it("does not match /_next/* static asset fetches", () => {
    expect(
      match({
        url: `${ORIGIN}/_next/static/chunks/main.js`,
        destination: "script",
      }),
    ).toBe(false);
  });

  it("does not match /uploads/* (server-side proxied attachment URLs)", () => {
    expect(match({ url: `${ORIGIN}/uploads/foo.png` })).toBe(false);
  });

  it("does not match /auth/* (OAuth callback / token endpoints)", () => {
    expect(match({ url: `${ORIGIN}/auth/callback?code=abc` })).toBe(false);
  });

  it("does not match RSC payloads (RSC: 1 header)", () => {
    // Next.js routes these to pages-rsc / pages-rsc-prefetch via defaultCache.
    expect(
      match({
        url: `${ORIGIN}/login?_rsc=abc`,
        init: { headers: { rsc: "1" } },
      }),
    ).toBe(false);
  });

  it("does not match the SW source itself (/sw.js)", () => {
    expect(match({ url: `${ORIGIN}/sw.js`, destination: "script" })).toBe(false);
  });

  it("does not match /manifest.webmanifest", () => {
    expect(match({ url: `${ORIGIN}/manifest.webmanifest` })).toBe(false);
  });

  it("does not match /robots.txt or /sitemap.xml", () => {
    expect(match({ url: `${ORIGIN}/robots.txt` })).toBe(false);
    expect(match({ url: `${ORIGIN}/sitemap.xml` })).toBe(false);
  });

  it("does not match /favicon.ico", () => {
    expect(match({ url: `${ORIGIN}/favicon.ico` })).toBe(false);
  });

  it("does not match arbitrary same-origin files with extensions", () => {
    expect(match({ url: `${ORIGIN}/icons/icon-192.png` })).toBe(false);
    expect(match({ url: `${ORIGIN}/data/foo.json` })).toBe(false);
    expect(match({ url: `${ORIGIN}/fonts/inter.woff2` })).toBe(false);
  });
});

describe("LEGACY_NAVIGATION_CACHES", () => {
  it("includes the serwist runtime caches that historically swallowed HTML", () => {
    expect(LEGACY_NAVIGATION_CACHES).toEqual(
      expect.arrayContaining([
        "others",
        "pages",
        "pages-rsc",
        "pages-rsc-prefetch",
      ]),
    );
  });

  it("does not include the precache (static assets must stay)", () => {
    for (const name of LEGACY_NAVIGATION_CACHES) {
      expect(name).not.toMatch(/serwist-precache/);
    }
  });
});

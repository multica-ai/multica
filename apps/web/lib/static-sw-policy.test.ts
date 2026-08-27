// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  BYPASS_PATH,
  HASHED_STATIC_PATH,
  LAST_DOCUMENT_PATH,
  PAGE_CACHE_NAME,
  PRECACHE_PATHS,
  REVALIDATE_PATH,
  STATIC_CACHE_NAME,
  shouldCacheStatically,
  staticAssetWorkerSource,
  staticSwAction,
} from "./static-sw-policy";

const origin = "https://www.multica.ai";

function action(
  path: string,
  extra: Partial<Parameters<typeof staticSwAction>[0]> = {},
) {
  return staticSwAction({
    method: "GET",
    url: `${origin}${path}`,
    pageOrigin: origin,
    ...extra,
  });
}

describe("staticSwAction — static assets first", () => {
  it.each([
    "/_next/static/chunks/app-abc123.js",
    "/_next/static/css/app.css",
    "/_next/static/media/font.woff2",
  ])("cache-first hashed chunks so return visits do not wait on the network: %s", (path) => {
    expect(action(path)).toBe("cache-first");
    expect(
      shouldCacheStatically({
        method: "GET",
        url: `${origin}${path}`,
        pageOrigin: origin,
      }),
    ).toBe(true);
  });

  it.each([
    "/icons/icon-192.png",
    "/icons/apple-touch-icon.png",
    "/favicon.svg",
  ])("revalidates launcher icons in the background: %s", (path) => {
    expect(action(path)).toBe("stale-revalidate");
  });
});

describe("staticSwAction — product documents", () => {
  it.each(["/acme/inbox", "/inbox", "/login", "/my-issues"])(
    "network-first with offline fallback: %s",
    (path) => {
      expect(action(path, { mode: "navigate" })).toBe(
        "network-first-document",
      );
    },
  );

  it.each([
    "/",
    "/homepage",
    "/download",
    "/changelog",
    "/about",
    "/usecases/agents",
    "/docs/intro",
  ])("leaves marketing HTML on the network: %s", (path) => {
    expect(action(path, { mode: "navigate" })).toBe("ignore");
  });
});

describe("staticSwAction — never intercept", () => {
  it.each(["/v1/issues", "/api/health", "/auth/callback", "/ws", "/uploads/x"])(
    "API, auth, websocket, uploads: %s",
    (path) => {
      expect(action(path)).toBe("ignore");
    },
  );

  it("ignores RSC flight even on a product path", () => {
    expect(
      action("/acme/inbox?_rsc=1a2b", {
        headers: { get: (name) => (name === "RSC" ? "1" : null) },
      }),
    ).toBe("ignore");
    expect(action("/acme/inbox?_rsc=1a2b")).toBe("ignore");
  });

  it("ignores router prefetch", () => {
    expect(
      action("/acme/inbox", {
        headers: {
          get: (name) => (name === "Next-Router-Prefetch" ? "1" : null),
        },
      }),
    ).toBe("ignore");
  });

  it("ignores POST, other origins, and the last-document hint path", () => {
    expect(action("/_next/static/chunks/app.js", { method: "POST" })).toBe(
      "ignore",
    );
    expect(
      staticSwAction({
        method: "GET",
        url: "https://cdn.example/_next/static/chunks/app.js",
        pageOrigin: origin,
      }),
    ).toBe("ignore");
    expect(action(LAST_DOCUMENT_PATH, { mode: "navigate" })).toBe("ignore");
  });

  it("does not treat a navigation to a hashed chunk as a document", () => {
    expect(
      action("/_next/static/chunks/app.js", { mode: "navigate" }),
    ).toBe("ignore");
  });
});

describe("staticAssetWorkerSource", () => {
  it("embeds the same matchers and cache buckets the policy tests exercise", () => {
    const source = staticAssetWorkerSource();

    expect(source).toContain(STATIC_CACHE_NAME);
    expect(source).toContain(PAGE_CACHE_NAME);
    expect(source).toContain(HASHED_STATIC_PATH.source);
    expect(source).toContain(REVALIDATE_PATH.source);
    expect(source).toContain(BYPASS_PATH.source);
    expect(source).toContain("cache-first");
    expect(source).toContain("stale-revalidate");
    expect(source).toContain("network-first-document");
    expect(source).toContain(LAST_DOCUMENT_PATH);
    for (const path of PRECACHE_PATHS) {
      expect(source).toContain(path);
    }
  });
});

import { describe, expect, it } from "vitest";

import {
  resolveBrowserApiBaseUrl,
  resolveBrowserWsUrl,
  resolveDevDocsUrl,
  resolveDevRemoteApiUrl,
  resolveDocsUrl,
  resolveRemoteApiUrl,
  runtimeRewriteDestination,
} from "./runtime-urls";

describe("resolveRemoteApiUrl", () => {
  it("prefers REMOTE_API_URL when explicitly configured", () => {
    expect(
      resolveRemoteApiUrl({
        REMOTE_API_URL: "http://backend:8080",
        NEXT_PUBLIC_API_URL: "http://localhost:19000",
        PORT: "18080",
      }),
    ).toBe("http://backend:8080");
  });

  it("uses NEXT_PUBLIC_API_URL when REMOTE_API_URL is unset", () => {
    expect(
      resolveRemoteApiUrl({
        NEXT_PUBLIC_API_URL: "http://localhost:19000",
        PORT: "18080",
      }),
    ).toBe("http://localhost:19000");
  });

  it("does not infer a backend URL from frontend or backend port env vars", () => {
    expect(
      resolveRemoteApiUrl({
        BACKEND_PORT: "28080",
        API_PORT: "38080",
        SERVER_PORT: "48080",
        PORT: "3000",
      }),
    ).toBeUndefined();
    expect(resolveRemoteApiUrl({ PORT: "3000" })).toBeUndefined();
  });

  it("does not use relative public API URLs for server-side rewrites", () => {
    expect(
      resolveRemoteApiUrl({
        NEXT_PUBLIC_API_URL: "/api",
      }),
    ).toBeUndefined();
  });

  // Same defect as the browser base, same fix: the rewrite target already
  // appends the full incoming pathname (`/api/**`, `/uploads/**`, `/ws`), so a
  // configured `/api` suffix would double it (MUL-5922).
  it("strips a trailing /api from server-side rewrite targets", () => {
    expect(
      resolveRemoteApiUrl({ REMOTE_API_URL: "http://backend:8080/api" }),
    ).toBe("http://backend:8080");
    expect(
      runtimeRewriteDestination("/api/config", {
        NEXT_PUBLIC_API_URL: "https://app.example.com/api",
      }),
    ).toBe("https://app.example.com/api/config");
    expect(
      runtimeRewriteDestination("/uploads/workspaces/a.png", {
        NEXT_PUBLIC_API_URL: "https://app.example.com/api",
      }),
    ).toBe("https://app.example.com/uploads/workspaces/a.png");
  });

  it("ignores whitespace-only or invalid backend URL values", () => {
    expect(
      resolveRemoteApiUrl({
        REMOTE_API_URL: "  ",
        NEXT_PUBLIC_API_URL: "ftp://api.example.com",
        PORT: "19080",
      }),
    ).toBeUndefined();
  });

  it("returns undefined when no API origin is configured", () => {
    expect(resolveRemoteApiUrl({})).toBeUndefined();
  });
});

describe("resolveDocsUrl", () => {
  it("uses DOCS_URL when configured", () => {
    expect(resolveDocsUrl({ DOCS_URL: " http://docs:4000/ " })).toBe(
      "http://docs:4000",
    );
  });

  it("returns undefined when no docs origin is configured", () => {
    expect(resolveDocsUrl({})).toBeUndefined();
  });

  it("ignores relative or invalid docs URL values", () => {
    expect(resolveDocsUrl({ DOCS_URL: "/docs" })).toBeUndefined();
    expect(resolveDocsUrl({ DOCS_URL: "ftp://docs.example.com" })).toBeUndefined();
  });
});

describe("browser runtime URLs", () => {
  it("exposes NEXT_PUBLIC_API_URL at server render time", () => {
    expect(
      resolveBrowserApiBaseUrl({
        NEXT_PUBLIC_API_URL: " https://api.example.com/ ",
      }),
    ).toBe("https://api.example.com");
  });

  // #6619: these two values used to reach the browser untouched and became the
  // XHR base, so every request went to `/api/api/**` and every avatar to
  // `<base>/uploads/**` — which the backend serves at the root, hence 404.
  it("rejects a relative public API URL so the browser stays same-origin", () => {
    expect(
      resolveBrowserApiBaseUrl({ NEXT_PUBLIC_API_URL: "/api" }),
    ).toBeUndefined();
    expect(
      resolveBrowserApiBaseUrl({ NEXT_PUBLIC_API_URL: "ftp://api.example.com" }),
    ).toBeUndefined();
  });

  it("strips a trailing /api from an absolute public API URL", () => {
    expect(
      resolveBrowserApiBaseUrl({
        NEXT_PUBLIC_API_URL: " https://app.example.com/api/ ",
      }),
    ).toBe("https://app.example.com");
  });

  it("keeps a non-/api path prefix so prefix-mounted backends still work", () => {
    expect(
      resolveBrowserApiBaseUrl({
        NEXT_PUBLIC_API_URL: "https://app.example.com/multica",
      }),
    ).toBe("https://app.example.com/multica");
  });

  it("does not mistake an `api` host for an /api path suffix", () => {
    expect(
      resolveBrowserApiBaseUrl({ NEXT_PUBLIC_API_URL: "http://api:8080" }),
    ).toBe("http://api:8080");
  });

  // The XHR base doubles as the base for `/uploads/**` (avatars, attachment
  // previews) via resolvePublicFileUrlWithBase in packages/core. Assert the
  // joined result, not just the base, because that join is what users saw fail.
  it("produces working /api and /uploads targets for every supported value", () => {
    const cases: Array<[string | undefined, string, string]> = [
      [undefined, "/api/issues", "/uploads/avatars/a.png"],
      ["/api", "/api/issues", "/uploads/avatars/a.png"],
      [
        "https://app.example.com/api",
        "https://app.example.com/api/issues",
        "https://app.example.com/uploads/avatars/a.png",
      ],
      [
        "https://api.example.com",
        "https://api.example.com/api/issues",
        "https://api.example.com/uploads/avatars/a.png",
      ],
    ];

    for (const [configured, expectedApi, expectedUpload] of cases) {
      // Mirrors the browser: CoreProvider defaults an absent base to "" and the
      // api client concatenates `${baseUrl}${path}`.
      const base =
        resolveBrowserApiBaseUrl({ NEXT_PUBLIC_API_URL: configured }) ?? "";
      expect(`${base}/api/issues`).toBe(expectedApi);
      expect(`${base}/uploads/avatars/a.png`).toBe(expectedUpload);
    }
  });

  // Sub-path semantics are intentional, not incidental: the base is the mount
  // prefix, so HTTP and websocket traffic must share it (see tryDeriveWsUrl).
  it("derives browser websocket URL from the public API URL", () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: "https://api.example.com/base",
      }),
    ).toBe("wss://api.example.com/base/ws");
  });

  it("derives a root websocket URL once the /api suffix is stripped", () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: "https://app.example.com/api",
      }),
    ).toBe("wss://app.example.com/ws");
  });

  it("prefers an explicit browser websocket URL", () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: "https://api.example.com",
        NEXT_PUBLIC_WS_URL: " wss://ws.example.com/socket/ ",
      }),
    ).toBe("wss://ws.example.com/socket");
  });

  it("falls back to same-origin websocket derivation for relative public API URLs", () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: "/api",
      }),
    ).toBeUndefined();
  });
});

describe("runtimeRewriteDestination", () => {
  it("keeps same-origin fallback when no runtime upstreams are configured", () => {
    expect(runtimeRewriteDestination("/api/config", {})).toBeUndefined();
    expect(runtimeRewriteDestination("/auth/send-code", {})).toBeUndefined();
    expect(
      runtimeRewriteDestination("/uploads/workspaces/a.png", {}),
    ).toBeUndefined();
    expect(runtimeRewriteDestination("/ws", {})).toBeUndefined();
    expect(runtimeRewriteDestination("/docs/zh", {})).toBeUndefined();
  });

  it("keeps same-origin fallback for runtime API paths when only frontend PORT is configured", () => {
    expect(
      runtimeRewriteDestination("/api/config", {
        PORT: "3000",
      }),
    ).toBeUndefined();
  });

  it("does not rewrite runtime API paths to relative public API URLs", () => {
    expect(
      runtimeRewriteDestination("/api/config", {
        NEXT_PUBLIC_API_URL: "/api",
      }),
    ).toBeUndefined();
  });

  it("maps backend HTTP paths to the runtime API origin", () => {
    expect(
      runtimeRewriteDestination("/api/config", {
        REMOTE_API_URL: "http://backend:8080",
      }),
    ).toBe("http://backend:8080/api/config");
    expect(
      runtimeRewriteDestination("/auth/send-code", {
        REMOTE_API_URL: "http://backend:8080",
      }),
    ).toBe("http://backend:8080/auth/send-code");
    expect(
      runtimeRewriteDestination("/uploads/workspaces/a.png", {
        REMOTE_API_URL: "http://backend:8080",
      }),
    ).toBe("http://backend:8080/uploads/workspaces/a.png");
  });

  it("does not rewrite frontend auth callback pages", () => {
    expect(runtimeRewriteDestination("/auth/callback", {})).toBeUndefined();
    expect(
      runtimeRewriteDestination("/auth/hg-sso/callback", {}),
    ).toBeUndefined();
  });

  it("maps docs paths to the runtime docs origin", () => {
    expect(
      runtimeRewriteDestination("/docs/zh/agents", {
        DOCS_URL: "http://multica-docs:3000",
      }),
    ).toBe("http://multica-docs:3000/docs/zh/agents");
  });

  it("maps websocket paths to the runtime API origin", () => {
    expect(
      runtimeRewriteDestination("/ws", {
        REMOTE_API_URL: "http://backend:8080",
      }),
    ).toBe("http://backend:8080/ws");
  });
});

describe("dev-only fallbacks", () => {
  it("falls back to the conventional local backend port", () => {
    expect(resolveDevRemoteApiUrl({})).toBe("http://localhost:8080");
  });

  it("honors BACKEND_PORT for the dev backend fallback", () => {
    expect(resolveDevRemoteApiUrl({ BACKEND_PORT: "19080" })).toBe(
      "http://localhost:19080",
    );
  });

  it("ignores the frontend process PORT while honoring backend-specific aliases", () => {
    expect(resolveDevRemoteApiUrl({ PORT: "3000" })).toBe(
      "http://localhost:8080",
    );
    expect(resolveDevRemoteApiUrl({ API_PORT: "19082", PORT: "3000" })).toBe(
      "http://localhost:19082",
    );
    expect(
      resolveDevRemoteApiUrl({ SERVER_PORT: "19083", PORT: "3000" }),
    ).toBe("http://localhost:19083");
    expect(
      resolveDevRemoteApiUrl({ BACKEND_PORT: "19080", PORT: "3000" }),
    ).toBe("http://localhost:19080");
  });

  it("prefers configured origins over the dev fallbacks", () => {
    expect(
      resolveDevRemoteApiUrl({ REMOTE_API_URL: "http://backend:8080" }),
    ).toBe("http://backend:8080");
    expect(resolveDevDocsUrl({ DOCS_URL: "http://docs:4000" })).toBe(
      "http://docs:4000",
    );
  });

  it("falls back to the local docs port", () => {
    expect(resolveDevDocsUrl({})).toBe("http://localhost:4000");
  });
});

import { describe, expect, it } from "vitest";

import {
  resolveBrowserApiBaseUrl,
  resolveBrowserWsUrl,
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

  it("derives localhost backend URL from PORT when no API URL is set", () => {
    expect(resolveRemoteApiUrl({ PORT: "19080" })).toBe(
      "http://localhost:19080",
    );
  });

  it("supports explicit backend port aliases before PORT", () => {
    expect(resolveRemoteApiUrl({ BACKEND_PORT: "28080", PORT: "19080" })).toBe(
      "http://localhost:28080",
    );
    expect(resolveRemoteApiUrl({ API_PORT: "38080", PORT: "19080" })).toBe(
      "http://localhost:38080",
    );
    expect(resolveRemoteApiUrl({ SERVER_PORT: "48080", PORT: "19080" })).toBe(
      "http://localhost:48080",
    );
  });

  it("prefers backend port aliases by documented precedence", () => {
    expect(
      resolveRemoteApiUrl({
        BACKEND_PORT: "28080",
        API_PORT: "38080",
        SERVER_PORT: "48080",
        PORT: "19080",
      }),
    ).toBe("http://localhost:28080");

    expect(
      resolveRemoteApiUrl({
        API_PORT: "38080",
        SERVER_PORT: "48080",
        PORT: "19080",
      }),
    ).toBe("http://localhost:38080");

    expect(resolveRemoteApiUrl({ SERVER_PORT: "48080", PORT: "19080" })).toBe(
      "http://localhost:48080",
    );
  });

  it("ignores whitespace-only backend URL values", () => {
    expect(
      resolveRemoteApiUrl({
        REMOTE_API_URL: "  ",
        NEXT_PUBLIC_API_URL: "  ",
        BACKEND_PORT: "  ",
        API_PORT: "  ",
        SERVER_PORT: "  ",
        PORT: "19080",
      }),
    ).toBe("http://localhost:19080");

    expect(resolveRemoteApiUrl({ PORT: "  " })).toBe("http://localhost:8080");
  });

  it("falls back to the historical backend port when no env is configured", () => {
    expect(resolveRemoteApiUrl({})).toBe("http://localhost:8080");
  });
});

describe("resolveDocsUrl", () => {
  it("uses DOCS_URL when configured", () => {
    expect(resolveDocsUrl({ DOCS_URL: " http://docs:4000/ " })).toBe(
      "http://docs:4000",
    );
  });

  it("falls back to the local docs dev server", () => {
    expect(resolveDocsUrl({})).toBe("http://localhost:4000");
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

  it("derives browser websocket URL from the public API URL", () => {
    expect(
      resolveBrowserWsUrl({
        NEXT_PUBLIC_API_URL: "https://api.example.com/base",
      }),
    ).toBe("wss://api.example.com/base/ws");
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

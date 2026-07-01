import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { MULTICA_LOCALE_HEADER } from "./lib/locale-routing";
import { proxy } from "./proxy";

function request(path: string, cookie = "") {
  return new NextRequest(`https://app.multica.ai${path}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

function restoreEnv(key: string, value: string | undefined) {
  if (value === undefined) delete process.env[key];
  else process.env[key] = value;
}

describe("proxy", () => {
  it("rewrites API requests to the runtime API origin", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(request("/api/config?x=1"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://backend:8080/api/config?x=1",
      );
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("rewrites docs requests to the runtime docs origin", () => {
    const previous = process.env.DOCS_URL;
    process.env.DOCS_URL = "http://docs:4000";
    try {
      const res = proxy(request("/docs/zh/agents"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://docs:4000/docs/zh/agents",
      );
    } finally {
      restoreEnv("DOCS_URL", previous);
    }
  });

  it("rewrites websocket requests to the runtime API origin", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(request("/ws"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://backend:8080/ws",
      );
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("does not rewrite frontend auth callback pages", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(request("/auth/callback"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBeNull();
      expect(
        res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
      ).toBe("en");
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("redirects logged-in root visits to the last workspace", () => {
    const res = proxy(
      request("/", "multica_logged_in=1; last_workspace_slug=acme"),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://app.multica.ai/acme/issues",
    );
  });

  it("forwards locale on login requests", () => {
    const res = proxy(request("/login", "multica-locale=zh-Hans"));

    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
    expect(
      res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
    ).toBe("zh-Hans");
  });
});

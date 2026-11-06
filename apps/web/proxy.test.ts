import { describe, expect, it } from "vitest";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { NextRequest } from "next/server";
import { MULTICA_LOCALE_HEADER } from "./lib/locale-routing";
import { config, proxy } from "./proxy";

function makeRequest(
  path: string,
  cookies: Record<string, string> = {},
  host = "app.multica.test",
) {
  const cookieHeader = Object.entries(cookies)
    .map(([key, value]) => `${key}=${value}`)
    .join("; ");

  return new NextRequest(`https://${host}${path}`, {
    headers: cookieHeader ? { cookie: cookieHeader } : undefined,
  });
}

function redirectLocation(
  path: string,
  cookies: Record<string, string> = {},
  host?: string,
) {
  return proxy(makeRequest(path, cookies, host)).headers.get("location");
}

function restoreEnv(key: string, value: string | undefined) {
  if (value === undefined) delete process.env[key];
  else process.env[key] = value;
}

function withoutRuntimeUpstreams(run: () => void) {
  const previousRemoteApiUrl = process.env.REMOTE_API_URL;
  const previousDocsUrl = process.env.DOCS_URL;
  const previousPublicApiUrl = process.env.NEXT_PUBLIC_API_URL;
  const previousPort = process.env.PORT;
  delete process.env.REMOTE_API_URL;
  delete process.env.DOCS_URL;
  delete process.env.NEXT_PUBLIC_API_URL;
  process.env.PORT = "3000";

  try {
    run();
  } finally {
    restoreEnv("REMOTE_API_URL", previousRemoteApiUrl);
    restoreEnv("DOCS_URL", previousDocsUrl);
    restoreEnv("NEXT_PUBLIC_API_URL", previousPublicApiUrl);
    restoreEnv("PORT", previousPort);
  }
}

describe("proxy legacy workspace route redirects", () => {
  const sessionCookies = {
    multica_logged_in: "1",
    last_workspace_slug: "acme",
  };

  it.each([
    ["issues", "/acme/issues"],
    ["projects", "/acme/projects"],
    ["agents", "/acme/agents"],
    ["squads", "/acme/squads"],
    ["inbox", "/acme/inbox"],
    ["my-issues", "/acme/my-issues"],
    ["autopilots", "/acme/autopilots"],
    ["runtimes", "/acme/runtimes"],
    ["skills", "/acme/skills"],
    ["settings", "/acme/settings"],
    ["usage", "/acme/usage"],
  ])(
    "redirects legacy /%s URLs through the last workspace slug",
    (segment, expectedPath) => {
      expect(redirectLocation(`/${segment}?tab=all`, sessionCookies)).toBe(
        `https://app.multica.test${expectedPath}?tab=all`,
      );
    },
  );

  it("preserves nested legacy paths and query strings", () => {
    expect(
      redirectLocation("/squads/squad-123?view=members", sessionCookies),
    ).toBe("https://app.multica.test/acme/squads/squad-123?view=members");
  });

  it.each([undefined, "previous-account"])(
    "sends logged-out legacy URLs to login with the original path and query (last workspace %s)",
    (lastSlug) => {
      const cookies: Record<string, string> = lastSlug ? { last_workspace_slug: lastSlug } : {};
      const location = new URL(redirectLocation("/usage?tab=billing", cookies)!);
      expect(location.pathname).toBe("/login");
      expect(location.searchParams.get("next")).toBe("/usage?tab=billing");
      expect(location.searchParams.has("tab")).toBe(false);
    },
  );

  it("passes an authenticated legacy link to login for workspace resolution", () => {
    expect(
      redirectLocation("/squads?view=members", { multica_logged_in: "1" }),
    ).toBe("https://app.multica.test/login?next=%2Fsquads%3Fview%3Dmembers");
  });

  it.each(["multica.ai", "www.multica.ai"])(
    "resolves a slugless session off the marketing host %s instead of stranding it",
    (host) => {
      expect(
        redirectLocation("/inbox", { multica_logged_in: "1" }, host),
      ).toBe(`https://${host}/login?next=%2Finbox`);
    },
  );

  it("does not redirect workspace-scoped URLs whose first segment is already a slug", () => {
    expect(redirectLocation("/acme/squads", sessionCookies)).toBeNull();
  });

  it("redirects app-host root URLs to the last workspace", () => {
    expect(redirectLocation("/", sessionCookies)).toBe(
      "https://app.multica.test/acme/issues",
    );
  });

  it.each(["multica.ai", "www.multica.ai"])(
    "does not redirect public marketing root on %s",
    (host) => {
      expect(redirectLocation("/", sessionCookies, host)).toBeNull();
    },
  );

  it("still redirects explicit legacy app routes on the public marketing host", () => {
    expect(redirectLocation("/issues/ABC-123", sessionCookies, "multica.ai")).toBe(
      "https://multica.ai/acme/issues/ABC-123",
    );
  });
});

describe("proxy runtime upstream rewrites", () => {
  it("matches globally versioned Plugin API requests", () => {
    expect(config.matcher).toContain("/v1/:path*");
  });

  it("does not rewrite API requests when no runtime API origin is configured", () => {
    withoutRuntimeUpstreams(() => {
      const res = proxy(makeRequest("/api/config?x=1"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBeNull();
      expect(
        res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
      ).toBe("en");
    });
  });

  it("does not rewrite Plugin API requests when no runtime API origin is configured", () => {
    withoutRuntimeUpstreams(() => {
      const res = proxy(makeRequest("/v1/context?x=1"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBeNull();
    });
  });

  it("does not rewrite docs requests when no runtime docs origin is configured", () => {
    withoutRuntimeUpstreams(() => {
      const res = proxy(makeRequest("/docs/zh"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBeNull();
      expect(
        res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
      ).toBe("en");
    });
  });

  it("rewrites API requests to the runtime API origin", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(makeRequest("/api/config?x=1"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://backend:8080/api/config?x=1",
      );
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("rewrites the CLI health probe to the runtime API origin", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(makeRequest("/health"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://backend:8080/health",
      );
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("rewrites Plugin API requests to the runtime API origin", () => {
    const previous = process.env.REMOTE_API_URL;
    process.env.REMOTE_API_URL = "http://backend:8080";
    try {
      const res = proxy(makeRequest("/v1/issues/MUL-6581?x=1"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBe(
        "http://backend:8080/v1/issues/MUL-6581?x=1",
      );
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });

  it("rewrites docs requests to the runtime docs origin", () => {
    const previous = process.env.DOCS_URL;
    process.env.DOCS_URL = "http://docs:4000";
    try {
      const res = proxy(makeRequest("/docs/zh/agents"));

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
      const res = proxy(makeRequest("/ws"));

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
      const res = proxy(makeRequest("/auth/callback"));

      expect(res.status).toBe(200);
      expect(res.headers.get("x-middleware-rewrite")).toBeNull();
      expect(
        res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
      ).toBe("en");
    } finally {
      restoreEnv("REMOTE_API_URL", previous);
    }
  });
});

describe("proxy root and locale handling", () => {
  it("redirects logged-in root visits to the last workspace", () => {
    const res = proxy(
      makeRequest("/", {
        multica_logged_in: "1",
        last_workspace_slug: "acme",
      }),
    );

    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(
      "https://app.multica.test/acme/issues",
    );
  });

  it("forwards locale on login requests", () => {
    const res = proxy(makeRequest("/login", { "multica-locale": "zh-Hans" }));

    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
    expect(
      res.headers.get(`x-middleware-request-${MULTICA_LOCALE_HEADER}`),
    ).toBe("zh-Hans");
  });
});


describe("upload route bypasses the buffering proxy", () => {
  it.each(["/api/upload-file", "/api/upload-file/"])(
    "excludes %s from Proxy matching",
    (url) => expect(unstable_doesMiddlewareMatch({ config, url })).toBe(false),
  );

  it.each(["/api", "/api/config", "/api/upload-file-extra", "/v1/issues", "/auth/token", "/docs/intro", "/login"])(
    "keeps the existing proxy behavior for %s",
    (url) => expect(unstable_doesMiddlewareMatch({ config, url })).toBe(true),
  );
});

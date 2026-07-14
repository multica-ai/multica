import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

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
  ])("redirects legacy /%s URLs through the last workspace slug", (segment, expectedPath) => {
    expect(redirectLocation(`/${segment}?tab=all`, sessionCookies)).toBe(
      `https://app.multica.test${expectedPath}?tab=all`,
    );
  });

  it("preserves nested legacy paths and query strings", () => {
    expect(
      redirectLocation("/squads/squad-123?view=members", sessionCookies),
    ).toBe("https://app.multica.test/acme/squads/squad-123?view=members");
  });

  it("sends logged-out legacy URLs to login", () => {
    expect(redirectLocation("/usage?tab=billing")).toBe(
      "https://app.multica.test/login?tab=billing",
    );
  });

  it("sends logged-in legacy URLs without a last workspace cookie to root", () => {
    expect(
      redirectLocation("/squads", { multica_logged_in: "1" }),
    ).toBe("https://app.multica.test/");
  });

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

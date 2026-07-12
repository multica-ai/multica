import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "./proxy";

function makeRequest(path: string, cookies: Record<string, string> = {}) {
  const cookieHeader = Object.entries(cookies)
    .map(([key, value]) => `${key}=${value}`)
    .join("; ");

  return new NextRequest(`https://app.multica.test${path}`, {
    headers: cookieHeader ? { cookie: cookieHeader } : undefined,
  });
}

function redirectLocation(path: string, cookies: Record<string, string> = {}) {
  return proxy(makeRequest(path, cookies)).headers.get("location");
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
});

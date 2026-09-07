import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { proxy } from "../proxy";
import manifest, { PWA_START_URL } from "./manifest";

function launch(cookies: Record<string, string>, host = "www.multica.ai") {
  const cookieHeader = Object.entries(cookies)
    .map(([key, value]) => `${key}=${value}`)
    .join("; ");

  return proxy(
    new NextRequest(`https://${host}${PWA_START_URL}`, {
      headers: cookieHeader ? { cookie: cookieHeader } : undefined,
    }),
  ).headers.get("location");
}

describe("web app manifest", () => {

  it("does not launch at the root path", () => {
    // The official marketing hosts keep "/" on the public site even for a
    // signed-in session, so an installed app pointed there opens the landing
    // page instead of the product.
    expect(manifest().start_url).not.toBe("/");
  });

  it("launches into the last workspace for a signed-in session", () => {
    expect(
      launch({ multica_logged_in: "1", last_workspace_slug: "acme" }),
    ).toContain("/acme/inbox");
  });

  it("launches into login when there is no session", () => {
    expect(launch({})).toContain("/login");
  });

  it("never launches onto the marketing site for a session with no known workspace", () => {
    // Reachable whenever `multica_logged_in` outlives `last_workspace_slug`:
    // a member who signed up but has not opened a workspace yet, or cleared
    // cookies. The proxy used to bounce this state to "/", which the official
    // marketing hosts keep on the public site — so the installed app opened
    // the landing page with no URL bar to escape it.
    const target = launch({ multica_logged_in: "1" });

    expect(target).toContain("/login");
    expect(new URL(target ?? "", "https://www.multica.ai").pathname).not.toBe(
      "/",
    );
  });

  it("points every shortcut at a path that resolves the same way", () => {
    const shortcuts = manifest().shortcuts ?? [];

    expect(shortcuts.length).toBeGreaterThan(0);
    for (const shortcut of shortcuts) {
      const resolve = (cookie: string) =>
        proxy(
          new NextRequest(`https://www.multica.ai${shortcut.url}`, {
            headers: { cookie },
          }),
        ).headers.get("location");

      expect(
        resolve("multica_logged_in=1; last_workspace_slug=acme"),
      ).toContain(`/acme${shortcut.url}`);
      // Same three states as start_url — a shortcut is a launcher entry too.
      expect(resolve("multica_logged_in=1")).toContain("/login");
      expect(resolve("")).toContain("/login");
    }
  });
});

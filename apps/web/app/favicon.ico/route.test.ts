// @vitest-environment node
import { describe, expect, it } from "vitest";
import { dynamic, GET } from "./route";

describe("GET /favicon.ico", () => {
  it("redirects to the SVG favicon", () => {
    const response = GET();

    expect(response.status).toBe(308);
    expect(response.headers.get("location")).toBe("/favicon.svg");
  });

  it("keeps the location relative so it survives a reverse proxy", () => {
    // Regression: the route used to build the location from `request.url`,
    // which on a self-hosted deployment is the server's listen address
    // (`HOSTNAME=0.0.0.0` / `PORT=3000` in Dockerfile.web) rather than the
    // origin the client used. That shipped `http://0.0.0.0:3000/favicon.svg`
    // to every browser — a dead link, and the internal bind address leaked.
    const location = GET().headers.get("location") ?? "";

    expect(location.startsWith("/")).toBe(true);
    expect(location).not.toMatch(/^https?:\/\//);
  });

  it("stays off the prerender cache path", () => {
    // Regression: `GET()` takes no arguments, so nothing marks the route
    // dynamic the way the old `request.url` read did. Next then tried to cache
    // a zero-byte body, which the prerender LRU rejects — one
    // "calculateSize returned 0" error per request, and every browser asks for
    // /favicon.ico unprompted.
    //
    // This asserts the export, not the framework behaviour it buys: a unit test
    // cannot observe Next's caching lifecycle. The behaviour itself is checked
    // against a production build (`pnpm build && pnpm start`), where the fix
    // removes both `cache-control: s-maxage=31536000` and the error.
    expect(dynamic).toBe("force-dynamic");
  });
});

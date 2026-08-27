// @vitest-environment node
import { describe, expect, it } from "vitest";
import { CACHE_NAME } from "@/lib/static-sw-policy";
import { dynamic, GET } from "./route";

describe("GET /sw.js", () => {
  it("serves the worker as JavaScript that browsers must revalidate", async () => {
    const response = GET();
    const body = await response.text();

    expect(response.headers.get("Content-Type")).toMatch(/javascript/);
    expect(response.headers.get("Cache-Control")).toBe("no-cache");
    expect(response.headers.get("Service-Worker-Allowed")).toBe("/");
    expect(body).toContain(CACHE_NAME);
  });

  it("stays off the prerender cache path", () => {
    expect(dynamic).toBe("force-dynamic");
  });
});

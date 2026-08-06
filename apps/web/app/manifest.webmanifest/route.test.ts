import { afterEach, describe, expect, it, vi } from "vitest";
import { GET } from "./route";

describe("GET /manifest.webmanifest", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("returns 404 outside production", () => {
    vi.stubEnv("NODE_ENV", "development");

    expect(GET().status).toBe(404);
  });

  it("serves the install manifest in production", async () => {
    vi.stubEnv("NODE_ENV", "production");

    const response = GET();

    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toBe(
      "application/manifest+json",
    );
    await expect(response.json()).resolves.toMatchObject({
      id: "/",
      start_url: "/?source=pwa",
      display: "standalone",
    });
  });
});

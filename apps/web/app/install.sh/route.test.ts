import { afterEach, describe, expect, it, vi } from "vitest";

import { GET } from "./route";

describe("GET /install.sh", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("serves the hosted install script", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("#!/usr/bin/env bash\necho multica\n", {
        status: 200,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await GET();

    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toContain("text/x-shellscript");
    await expect(response.text()).resolves.toContain("echo multica");
    expect(fetchMock).toHaveBeenCalledWith(
      "https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh",
      expect.objectContaining({
        next: { revalidate: 300 },
      }),
    );
  });

  it("returns a plain 502 when the upstream installer is unavailable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("not found", { status: 404 })),
    );

    const response = await GET();

    expect(response.status).toBe(502);
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.text()).resolves.toContain(
      "Failed to fetch Multica installer",
    );
  });
});

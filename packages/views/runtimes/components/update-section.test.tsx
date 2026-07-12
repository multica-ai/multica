import { beforeEach, describe, expect, it, vi } from "vitest";

const { getLatestRuntimeVersion } = vi.hoisted(() => ({
  getLatestRuntimeVersion: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: { getLatestRuntimeVersion },
}));

import { fetchLatestVersion, resetLatestVersionCache } from "./update-section";

describe("runtime latest version lookup", () => {
  beforeEach(() => {
    getLatestRuntimeVersion.mockReset();
    resetLatestVersionCache();
  });

  it("uses the server-owned distribution version", async () => {
    getLatestRuntimeVersion.mockResolvedValue("v1.2.3");
    await expect(fetchLatestVersion()).resolves.toBe("v1.2.3");
    expect(getLatestRuntimeVersion).toHaveBeenCalledOnce();
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../api", () => ({
  api: {
    getLatestCliRelease: vi.fn(),
  },
}));

import { api } from "../api";
import {
  LATEST_CLI_VERSION_ENDPOINT,
  fetchLatestCliVersion,
} from "./update-source";

const mockedGetLatestCliRelease = api.getLatestCliRelease as ReturnType<typeof vi.fn>;

describe("CLI update source", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("uses the server runtime latest-version endpoint as the source of truth", () => {
    expect(LATEST_CLI_VERSION_ENDPOINT).toBe("/api/runtimes/latest-version");
  });

  it("fetches the latest CLI tag via the backend API", async () => {
    mockedGetLatestCliRelease.mockResolvedValue({
      repo: "ethanturk/multica",
      tag_name: "v1.2.3",
    });

    await expect(fetchLatestCliVersion()).resolves.toBe("v1.2.3");
    expect(mockedGetLatestCliRelease).toHaveBeenCalledTimes(1);
  });

  it("returns null when the backend API fails", async () => {
    mockedGetLatestCliRelease.mockRejectedValue(new Error("boom"));

    await expect(fetchLatestCliVersion()).resolves.toBeNull();
  });
});

import { describe, expect, it, vi } from "vitest";
import {
  CLI_LATEST_RELEASE_URL,
  CLI_UPDATE_REPO,
  fetchLatestCliVersion,
} from "./update-source";

describe("CLI update source", () => {
  it("defaults frontend update checks to the fork releases", () => {
    expect(CLI_UPDATE_REPO).toBe("ethanturk/multica");
    expect(CLI_LATEST_RELEASE_URL).toBe(
      "https://api.github.com/repos/ethanturk/multica/releases/latest",
    );
  });

  it("fetches the latest CLI tag from the configured repo", async () => {
    const fetcher = vi.fn(async () => ({
      ok: true,
      json: async () => ({ tag_name: "v1.2.3" }),
    })) as unknown as typeof fetch;

    await expect(fetchLatestCliVersion(fetcher)).resolves.toBe("v1.2.3");
    expect(fetcher).toHaveBeenCalledWith(CLI_LATEST_RELEASE_URL, {
      headers: { Accept: "application/vnd.github+json" },
    });
  });
});

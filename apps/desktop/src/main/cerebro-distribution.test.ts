import { describe, expect, it } from "vitest";

import {
  CEREBRO_CLI_RELEASE_DOWNLOAD_URL,
  CEREBRO_DESKTOP_PUBLISH_CONFIG,
} from "./cerebro-distribution";

describe("Cerebro distribution coordinates", () => {
  it("uses the public Firtal channel for CLI bootstrap", () => {
    expect(CEREBRO_CLI_RELEASE_DOWNLOAD_URL).toBe(
      "https://github.com/firtal-group/homebrew-tap/releases/latest/download",
    );
  });

  it("uses the public Firtal channel for Electron updates", () => {
    expect(CEREBRO_DESKTOP_PUBLISH_CONFIG).toEqual({
      provider: "github",
      owner: "firtal-group",
      repo: "homebrew-tap",
    });
  });
});

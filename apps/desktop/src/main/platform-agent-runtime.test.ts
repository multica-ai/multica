import { join } from "path";
import { describe, expect, it } from "vitest";

import {
  PLATFORM_AGENT_CLI_PATH_ENV,
  bundledPlatformAgentPath,
  withBundledPlatformAgentPath,
} from "./platform-agent-runtime";

describe("bundledPlatformAgentPath", () => {
  it("resolves the development app's bundled Platform Agent CLI", () => {
    expect(bundledPlatformAgentPath("/workspace/apps/desktop", "darwin")).toBe(
      join("/workspace/apps/desktop", "resources", "bin", "platform-agent-cli"),
    );
  });

  it("resolves packaged apps from app.asar.unpacked", () => {
    expect(
      bundledPlatformAgentPath(
        "/Applications/Multica.app/Contents/Resources/app.asar",
        "darwin",
      ),
    ).toBe(
      "/Applications/Multica.app/Contents/Resources/app.asar.unpacked/resources/bin/platform-agent-cli",
    );
  });

  it("uses the Windows executable suffix", () => {
    expect(bundledPlatformAgentPath("C:/Multica/resources/app.asar", "win32")).toBe(
      "C:/Multica/resources/app.asar.unpacked/resources/bin/platform-agent-cli.exe",
    );
  });
});

describe("withBundledPlatformAgentPath", () => {
  it("overwrites an inherited path when the bundled CLI exists", () => {
    const result = withBundledPlatformAgentPath(
      { PATH: "/usr/bin", [PLATFORM_AGENT_CLI_PATH_ENV]: "/stale/platform-agent-cli" },
      "/workspace/apps/desktop",
      "linux",
      () => true,
    );

    expect(result).toEqual({
      PATH: "/usr/bin",
      [PLATFORM_AGENT_CLI_PATH_ENV]: "/workspace/apps/desktop/resources/bin/platform-agent-cli",
    });
  });

  it("removes an inherited path when the bundled CLI is absent without mutating input", () => {
    const sourceEnv = {
      PATH: "/usr/bin",
      [PLATFORM_AGENT_CLI_PATH_ENV]: "/stale/platform-agent-cli",
    };

    const result = withBundledPlatformAgentPath(
      sourceEnv,
      "/workspace/apps/desktop",
      "linux",
      () => false,
    );

    expect(result).toEqual({ PATH: "/usr/bin" });
    expect(sourceEnv).toEqual({
      PATH: "/usr/bin",
      [PLATFORM_AGENT_CLI_PATH_ENV]: "/stale/platform-agent-cli",
    });
    expect(result).not.toBe(sourceEnv);
  });
});

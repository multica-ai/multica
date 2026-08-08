import { join } from "path";
import { describe, expect, it } from "vitest";

import {
  PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV,
  PLATFORM_AGENT_CLI_PATH_ENV,
  PLATFORM_AGENT_MODE_ENV,
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
      [PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV]: "1",
      [PLATFORM_AGENT_CLI_PATH_ENV]: "/workspace/apps/desktop/resources/bin/platform-agent-cli",
      [PLATFORM_AGENT_MODE_ENV]: "mock",
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

    expect(result).toEqual({
      PATH: "/usr/bin",
      [PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV]: "1",
      [PLATFORM_AGENT_MODE_ENV]: "mock",
    });
    expect(sourceEnv).toEqual({
      PATH: "/usr/bin",
      [PLATFORM_AGENT_CLI_PATH_ENV]: "/stale/platform-agent-cli",
    });
    expect(result).not.toBe(sourceEnv);
  });

  it("removes differently-cased inherited path keys before entering bundled-only mode", () => {
    const result = withBundledPlatformAgentPath(
      {
        PATH: "C:/Windows/System32",
        multica_platform_agent_cli_path: "C:/stale/platform-agent-cli.exe",
        Multica_Platform_Agent_Cli_Desktop_Bundled_Only: "0",
      },
      "C:/Multica/resources/app.asar",
      "win32",
      () => false,
    );

    expect(result).toEqual({
      PATH: "C:/Windows/System32",
      [PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY_ENV]: "1",
      [PLATFORM_AGENT_MODE_ENV]: "mock",
    });
    expect(
      Object.keys(result).filter(
        (key) => key.toUpperCase() === PLATFORM_AGENT_CLI_PATH_ENV,
      ),
    ).toEqual([]);
  });

  it("preserves an explicit HTTP mode for daemon start children", () => {
    const result = withBundledPlatformAgentPath(
      {
        PATH: "/usr/bin",
        [PLATFORM_AGENT_MODE_ENV]: "http",
      },
      "/workspace/apps/desktop",
      "linux",
      () => true,
    );

    expect(result[PLATFORM_AGENT_MODE_ENV]).toBe("http");
  });

  it("only defaults the mode when the environment key is absent", () => {
    const result = withBundledPlatformAgentPath(
      {
        PATH: "/usr/bin",
        [PLATFORM_AGENT_MODE_ENV]: "",
      },
      "/workspace/apps/desktop",
      "linux",
      () => true,
    );

    expect(result[PLATFORM_AGENT_MODE_ENV]).toBe("");
  });

  it("canonicalizes an explicit mixed-case Windows mode key for probe children", () => {
    const result = withBundledPlatformAgentPath(
      {
        Path: "C:/Windows/System32",
        Platform_Agent_Mode: "http",
      },
      "C:/Multica/resources/app.asar",
      "win32",
      () => true,
    );

    expect(result[PLATFORM_AGENT_MODE_ENV]).toBe("http");
    expect(
      Object.keys(result).filter(
        (key) => key.toUpperCase() === PLATFORM_AGENT_MODE_ENV,
      ),
    ).toEqual([PLATFORM_AGENT_MODE_ENV]);
  });
});

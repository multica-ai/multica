// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  daemonCommands,
  normalizeCommandURL,
  type DaemonSetupConfig,
} from "./commands";

describe("normalizeCommandURL", () => {
  it("trims and strips trailing slashes", () => {
    expect(normalizeCommandURL("  https://api.example.com/// ")).toBe(
      "https://api.example.com",
    );
    expect(normalizeCommandURL("")).toBe("");
    expect(normalizeCommandURL(undefined)).toBe("");
  });
});

describe("daemonCommands", () => {
  it("only returns Cloud commands for a confirmed Cloud deployment", () => {
    const result = daemonCommands({ status: "ready", target: "cloud" });
    expect(result.status).toBe("ready");
    if (result.status !== "ready") {
      throw new Error("expected confirmed Cloud commands");
    }
    expect(result.setupCmd).toBe("multica setup");
    expect(result.tokenCmd).toContain("server_url https://api.multica.ai");
    expect(result.tokenCmd).toContain("app_url https://multica.ai");
  });

  it("returns self-host commands with the configured URLs", () => {
    const result = daemonCommands({
      status: "ready",
      target: "self_host",
      serverUrl: " https://api.example.com/// ",
      appUrl: "https://app.example.com/",
    });
    expect(result.status).toBe("ready");
    if (result.status !== "ready") {
      throw new Error("expected self-host commands");
    }
    expect(result.setupCmd).toBe(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(result.tokenCmd).toContain(
      "multica config set server_url https://api.example.com",
    );
    expect(result.tokenCmd).toContain(
      "multica config set app_url https://app.example.com",
    );
    expect(result.tokenCmd).not.toContain("multica.ai");
  });

  it.each<{ name: string; config: DaemonSetupConfig; status: string }>([
    {
      name: "initial load",
      config: { status: "loading", target: "unknown" },
      status: "loading",
    },
    {
      name: "loading despite a previous target",
      config: { status: "loading", target: "cloud" },
      status: "loading",
    },
    {
      name: "request failure",
      config: { status: "error", target: "unknown" },
      status: "error",
    },
    {
      name: "failure despite a previous target",
      config: { status: "error", target: "cloud" },
      status: "error",
    },
    {
      name: "missing target",
      config: { status: "ready", target: "unknown" },
      status: "unknown",
    },
    {
      name: "unknown target with URLs",
      config: {
        status: "ready", target: "unknown",
        serverUrl: "https://api.example.com", appUrl: "https://app.example.com",
      },
      status: "unknown",
    },
    {
      name: "missing both self-host URLs",
      config: { status: "ready", target: "self_host" },
      status: "incomplete",
    },
    {
      name: "missing app URL",
      config: { status: "ready", target: "self_host", serverUrl: "https://api.example.com" },
      status: "incomplete",
    },
    {
      name: "missing server URL",
      config: { status: "ready", target: "self_host", appUrl: "https://app.example.com" },
      status: "incomplete",
    },
    {
      name: "blank self-host URL",
      config: {
        status: "ready", target: "self_host",
        serverUrl: " / ", appUrl: "https://app.example.com",
      },
      status: "incomplete",
    },
  ])("offers no setup or token command for $name", ({ config, status }) => {
    expect(daemonCommands(config)).toEqual({ status });
  });
});

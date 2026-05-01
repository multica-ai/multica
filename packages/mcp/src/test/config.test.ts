import { describe, expect, it } from "vitest";
import { writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { ConfigError, deriveHttpBase, loadConfig } from "../config.js";

describe("deriveHttpBase", () => {
  it("converts wss:// to https:// and strips /ws suffix", () => {
    expect(deriveHttpBase("wss://api.example.com/ws")).toBe("https://api.example.com");
  });
  it("converts ws:// to http:// and strips /ws/ suffix", () => {
    expect(deriveHttpBase("ws://localhost:8080/ws/")).toBe("http://localhost:8080");
  });
  it("leaves plain https:// alone", () => {
    expect(deriveHttpBase("https://api.example.com")).toBe("https://api.example.com");
  });
  it("returns null for empty / undefined input", () => {
    expect(deriveHttpBase(undefined)).toBeNull();
    expect(deriveHttpBase("")).toBeNull();
    expect(deriveHttpBase("   ")).toBeNull();
  });
});

describe("loadConfig", () => {
  it("reads env vars when present", () => {
    const cfg = loadConfig({
      env: {
        MULTICA_API_URL: "https://api.example.com/",
        MULTICA_TOKEN: "mul_test",
        MULTICA_WORKSPACE_ID: "ws-1",
      },
      cliConfigPath: "/nonexistent/path/config.json",
    });
    expect(cfg).toEqual({
      apiUrl: "https://api.example.com",
      token: "mul_test",
      defaultWorkspaceId: "ws-1",
    });
  });

  it("falls back to ~/.multica/config.json when env is empty", () => {
    const dir = mkdtempSync(join(tmpdir(), "multica-mcp-test-"));
    const path = join(dir, "config.json");
    writeFileSync(
      path,
      JSON.stringify({
        server_url: "wss://api.example.com/ws",
        token: "mul_cli",
        workspace_id: "ws-cli",
      }),
    );
    try {
      const cfg = loadConfig({ env: {}, cliConfigPath: path });
      expect(cfg).toEqual({
        apiUrl: "https://api.example.com",
        token: "mul_cli",
        defaultWorkspaceId: "ws-cli",
      });
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("env wins over CLI config when both are set", () => {
    const dir = mkdtempSync(join(tmpdir(), "multica-mcp-test-"));
    const path = join(dir, "config.json");
    writeFileSync(
      path,
      JSON.stringify({ token: "from_cli", workspace_id: "ws-cli", server_url: "wss://cli.example/ws" }),
    );
    try {
      const cfg = loadConfig({
        env: { MULTICA_API_URL: "https://env.example", MULTICA_TOKEN: "from_env" },
        cliConfigPath: path,
      });
      expect(cfg.apiUrl).toBe("https://env.example");
      expect(cfg.token).toBe("from_env");
      // Workspace falls back since env didn't set one.
      expect(cfg.defaultWorkspaceId).toBe("ws-cli");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("throws ConfigError when no token is available anywhere", () => {
    expect(() =>
      loadConfig({
        env: { MULTICA_API_URL: "https://api.example.com" },
        cliConfigPath: "/nonexistent/config.json",
      }),
    ).toThrow(ConfigError);
  });

  it("throws ConfigError when no API URL is available anywhere", () => {
    expect(() =>
      loadConfig({
        env: { MULTICA_TOKEN: "mul_test" },
        cliConfigPath: "/nonexistent/config.json",
      }),
    ).toThrow(ConfigError);
  });

  it("ignores a malformed CLI config without crashing", () => {
    const dir = mkdtempSync(join(tmpdir(), "multica-mcp-test-"));
    const path = join(dir, "config.json");
    writeFileSync(path, "{ this is : not json");
    try {
      // Should fall through to env-based loading.
      const cfg = loadConfig({
        env: { MULTICA_API_URL: "https://api.example.com", MULTICA_TOKEN: "mul_env" },
        cliConfigPath: path,
      });
      expect(cfg.apiUrl).toBe("https://api.example.com");
      expect(cfg.token).toBe("mul_env");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

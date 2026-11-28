// @vitest-environment node
import { mkdtemp, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getPath: () => tmpdir() },
}));

import { loadRuntimeConfig } from "./runtime-config-loader";

describe("loadRuntimeConfig", () => {
  it("uses dev env and ignores desktop.json during electron-vite dev", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: "https://prod.example.com" }),
    );

    await expect(
      loadRuntimeConfig({
        isDev: true,
        configPath,
        env: {
          apiUrl: "http://localhost:8080",
          wsUrl: "ws://localhost:8080/ws",
          appUrl: "http://localhost:3000",
        },
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "http://localhost:8080",
        wsUrl: "ws://localhost:8080/ws",
        appUrl: "http://localhost:3000",
      },
    });
  });

  it("uses cloud defaults when packaged config is absent", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        configPath: join(dir, "missing.json"),
        env: {},
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "https://api.multica.ai",
        wsUrl: "wss://api.multica.ai/ws",
        appUrl: "https://multica.ai",
      },
    });
  });

  it("parses a valid packaged desktop.json", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: "https://api.example.com" }),
    );

    await expect(
      loadRuntimeConfig({ isDev: false, configPath, env: {} }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "https://api.example.com",
        wsUrl: "wss://api.example.com/ws",
        appUrl: "https://example.com",
      },
    });
  });

  it("fails closed when packaged desktop.json is invalid", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(configPath, "{");

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toContain(configPath);
      expect(result.error.message).toContain("Invalid desktop runtime config JSON");
    }
  });

  describe("with a server URL baked in at package time", () => {
    it("uses the baked server when packaged config is absent", async () => {
      const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
      await expect(
        loadRuntimeConfig({
          isDev: false,
          configPath: join(dir, "missing.json"),
          env: {},
          defaultServerUrl: "https://multica.example.com",
        }),
      ).resolves.toEqual({
        ok: true,
        config: {
          schemaVersion: 1,
          apiUrl: "https://multica.example.com",
          wsUrl: "wss://multica.example.com/ws",
          appUrl: "https://multica.example.com",
        },
      });
    });

    it("still lets desktop.json win", async () => {
      const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
      const configPath = join(dir, "desktop.json");
      await writeFile(
        configPath,
        JSON.stringify({ schemaVersion: 1, apiUrl: "https://api.example.com" }),
      );

      await expect(
        loadRuntimeConfig({
          isDev: false,
          configPath,
          env: {},
          defaultServerUrl: "https://multica.example.com",
        }),
      ).resolves.toEqual({
        ok: true,
        config: {
          schemaVersion: 1,
          apiUrl: "https://api.example.com",
          wsUrl: "wss://api.example.com/ws",
          appUrl: "https://example.com",
        },
      });
    });

    it("does not fall back to the baked server when desktop.json is invalid", async () => {
      const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
      const configPath = join(dir, "desktop.json");
      await writeFile(configPath, "{");

      const result = await loadRuntimeConfig({
        isDev: false,
        configPath,
        env: {},
        defaultServerUrl: "https://multica.example.com",
      });

      expect(result.ok).toBe(false);
    });

    it("ignores the baked server during electron-vite dev", async () => {
      const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
      const result = await loadRuntimeConfig({
        isDev: true,
        configPath: join(dir, "missing.json"),
        env: {},
        defaultServerUrl: "https://multica.example.com",
      });

      expect(result).toMatchObject({
        ok: true,
        config: { apiUrl: "http://localhost:8080" },
      });
    });

    it("reports an unusable baked server instead of connecting to Cloud", async () => {
      const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
      const result = await loadRuntimeConfig({
        isDev: false,
        configPath: join(dir, "missing.json"),
        env: {},
        defaultServerUrl: "not a url",
      });

      expect(result).toEqual({
        ok: false,
        error: {
          message:
            "Invalid desktop runtime config: MULTICA_DESKTOP_SERVER_URL must be a valid URL",
        },
      });
    });
  });
});

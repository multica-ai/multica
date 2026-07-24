import { mkdtemp, readFile, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { describe, expect, it, vi } from "vitest";

// The loader imports `app` from electron only for the default config path;
// tests always pass an explicit `configPath`, but the module still evaluates
// the electron package on import.
vi.mock("electron", () => ({
  app: {
    getPath: () => tmpdir(),
  },
}));

import { loadRuntimeConfig, saveDesktopServersState } from "./runtime-config-loader";

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
      servers: {
        activeServerId: "localhost-8080",
        servers: [
          {
            id: "localhost-8080",
            name: "localhost:8080",
            apiUrl: "http://localhost:8080",
            wsUrl: "ws://localhost:8080/ws",
            appUrl: "http://localhost:3000",
          },
        ],
        editable: false,
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
      servers: {
        activeServerId: "cloud",
        servers: [
          {
            id: "cloud",
            name: "Multica Cloud",
            apiUrl: "https://api.multica.ai",
            wsUrl: "wss://api.multica.ai/ws",
            appUrl: "https://multica.ai",
          },
        ],
        editable: true,
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
      servers: {
        activeServerId: "api-example-com",
        servers: [
          {
            id: "api-example-com",
            name: "api.example.com",
            apiUrl: "https://api.example.com",
            wsUrl: "wss://api.example.com/ws",
            appUrl: "https://example.com",
          },
        ],
        editable: true,
      },
    });
  });

  it("parses multi-server desktop.json and selects the active profile", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(
      configPath,
      JSON.stringify({
        schemaVersion: 1,
        activeServerId: "personal",
        servers: [
          {
            id: "cloud",
            name: "Multica Cloud",
            apiUrl: "https://api.multica.ai",
          },
          {
            id: "personal",
            name: "Personal",
            apiUrl: "http://127.0.0.1:28443",
          },
        ],
      }),
    );

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.config.apiUrl).toBe("http://127.0.0.1:28443");
    expect(result.servers.activeServerId).toBe("personal");
    expect(result.servers.servers).toHaveLength(2);
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

  it("persists multi-server state with backward-compatible top-level fields", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");

    const saved = await saveDesktopServersState({
      configPath,
      servers: {
        editable: true,
        activeServerId: "personal",
        servers: [
          {
            id: "cloud",
            name: "Multica Cloud",
            apiUrl: "https://api.multica.ai",
            wsUrl: "wss://api.multica.ai/ws",
            appUrl: "https://multica.ai",
          },
          {
            id: "personal",
            name: "Personal",
            apiUrl: "http://127.0.0.1:28443",
            wsUrl: "ws://127.0.0.1:28443/ws",
            appUrl: "http://127.0.0.1:28443",
          },
        ],
      },
    });

    expect(saved.config.apiUrl).toBe("http://127.0.0.1:28443");
    const raw = JSON.parse(await readFile(configPath, "utf-8"));
    expect(raw.apiUrl).toBe("http://127.0.0.1:28443");
    expect(raw.activeServerId).toBe("personal");
    expect(raw.servers).toHaveLength(2);
  });
});

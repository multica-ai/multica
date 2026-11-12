// @vitest-environment node
import { mkdtemp, readFile, readdir, stat, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getPath: () => tmpdir() },
}));

import { loadRuntimeConfig, updateDesktopConfig } from "./runtime-config-loader";

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

  it("rejects typoed runtime keys instead of silently falling back to cloud defaults", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-desktop-config-"));
    const configPath = join(dir, "desktop.json");
    await writeFile(configPath, JSON.stringify({ apiURL: "https://multica.example.com" }));

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toContain("apiUrl");
    }
  });
});

describe("updateDesktopConfig", () => {
  it("preserves unknown keys without freezing derived URLs", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-proxy-config-"));
    const path = join(dir, "desktop.json");
    const original = { schemaVersion: 1, apiUrl: "https://api.example.com", custom: { keep: true } };
    await writeFile(path, JSON.stringify(original));
    const result = await updateDesktopConfig({ proxyUrl: "http://proxy:8080", noProxy: "corp" }, path);
    expect(result.ok).toBe(true);
    expect(JSON.parse(await readFile(path, "utf-8"))).toEqual({ ...original, proxyUrl: "http://proxy:8080", noProxy: "corp" });
    expect(await readdir(dir)).toEqual(["desktop.json"]);
    if (process.platform !== "win32") expect((await stat(path)).mode & 0o777).toBe(0o600);
    await updateDesktopConfig({ proxyUrl: undefined, noProxy: undefined }, path);
    expect(JSON.parse(await readFile(path, "utf-8"))).toEqual(original);
  });

  it("creates a proxy-only config in a missing directory, and loads it on next launch", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-proxy-config-"));
    const path = join(dir, "nested", "desktop.json");
    expect((await updateDesktopConfig({ proxyUrl: "http://proxy:8080" }, path)).ok).toBe(true);
    expect(JSON.parse(await readFile(path, "utf-8"))).toEqual({ proxyUrl: "http://proxy:8080" });
    const loaded = await loadRuntimeConfig({ isDev: false, env: {}, configPath: path });
    expect(loaded).toMatchObject({ ok: true, config: { apiUrl: "https://api.multica.ai", proxyUrl: "http://proxy:8080" } });
  });

  it("keeps dev endpoints while loading saved proxy settings", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-proxy-config-"));
    const path = join(dir, "desktop.json");
    await updateDesktopConfig({ proxyUrl: "http://proxy:8080" }, path);
    expect(await loadRuntimeConfig({ isDev: true, env: { apiUrl: "http://localhost:9000" }, configPath: path }))
      .toMatchObject({ ok: true, config: { apiUrl: "http://localhost:9000", wsUrl: "ws://localhost:9000/ws", proxyUrl: "http://proxy:8080" } });
  });

  it("leaves malformed files intact", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-proxy-config-"));
    const path = join(dir, "desktop.json");
    await writeFile(path, "{");
    expect((await updateDesktopConfig({ proxyUrl: "http://proxy:8080" }, path)).ok).toBe(false);
    expect(await readFile(path, "utf-8")).toBe("{");
    expect(await readdir(dir)).toEqual(["desktop.json"]);
  });

  it("ignores packaged endpoint validation in dev while reading the proxy fields", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-proxy-config-"));
    const path = join(dir, "desktop.json");
    await writeFile(path, JSON.stringify({ apiUrl: "stale endpoint", proxyUrl: "http://proxy:8080" }));
    expect(await loadRuntimeConfig({ isDev: true, env: { apiUrl: "http://localhost:9000" }, configPath: path }))
      .toMatchObject({ ok: true, config: { apiUrl: "http://localhost:9000", proxyUrl: "http://proxy:8080" } });
  });
});

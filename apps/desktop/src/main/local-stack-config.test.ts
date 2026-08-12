// src/main/local-stack-config.test.ts
import { mkdtemp, writeFile } from "fs/promises";
import { join } from "path";
import { tmpdir } from "os";
import { describe, expect, it } from "vitest";
import { isLocalApiUrl, loadLocalStackConfig } from "./local-stack-config";

describe("loadLocalStackConfig", () => {
  it("returns null when the file is absent so the supervisor stays disabled", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-local-stack-"));
    await expect(loadLocalStackConfig(join(dir, "missing.json"))).resolves.toBeNull();
  });

  it("parses a complete config", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-local-stack-"));
    const path = join(dir, "config.json");
    await writeFile(
      path,
      JSON.stringify({
        repoDir: "/repo",
        composeFile: "docker-compose.selfhost.yml",
        backendPort: 8081,
      }),
    );
    await expect(loadLocalStackConfig(path)).resolves.toEqual({
      repoDir: "/repo",
      composeFile: "docker-compose.selfhost.yml",
      backendPort: 8081,
    });
  });

  it("defaults composeFile and backendPort when omitted", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-local-stack-"));
    const path = join(dir, "config.json");
    await writeFile(path, JSON.stringify({ repoDir: "/repo" }));
    await expect(loadLocalStackConfig(path)).resolves.toEqual({
      repoDir: "/repo",
      composeFile: "docker-compose.selfhost.yml",
      backendPort: 8080,
    });
  });

  it("throws on a config without repoDir", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-local-stack-"));
    const path = join(dir, "config.json");
    await writeFile(path, JSON.stringify({ backendPort: 8081 }));
    await expect(loadLocalStackConfig(path)).rejects.toThrow(/repoDir/);
  });

  it("throws on malformed JSON rather than silently disabling", async () => {
    const dir = await mkdtemp(join(tmpdir(), "multica-local-stack-"));
    const path = join(dir, "config.json");
    await writeFile(path, "{ not json");
    await expect(loadLocalStackConfig(path)).rejects.toThrow();
  });
});

describe("isLocalApiUrl", () => {
  it.each([
    ["http://localhost:8081", true],
    ["http://127.0.0.1:8081", true],
    ["http://[::1]:8081", true],
    ["https://api.multica.ai", false],
    ["https://multica-api.copilothub.ai", false],
    ["not a url", false],
  ])("%s -> %s", (url, expected) => {
    expect(isLocalApiUrl(url)).toBe(expected);
  });
});

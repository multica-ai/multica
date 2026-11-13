// @vitest-environment node
import { spawnSync } from "node:child_process";
import {
  copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync,
  rmSync, symlinkSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const sourceScripts = [
  resolve(process.cwd(), "scripts"),
  resolve(process.cwd(), "apps/desktop/scripts"),
].find((dir) => existsSync(join(dir, "bundle-cli.mjs")));
const roots = [];

afterEach(() => {
  while (roots.length) rmSync(roots.pop(), { recursive: true, force: true });
});

function fixture() {
  const root = mkdtempSync(join(tmpdir(), "multica-package-cli-"));
  roots.push(root);
  const scripts = join(root, "apps", "desktop", "scripts");
  const bin = join(root, "tools");
  const resources = join(root, "apps", "desktop", "resources", "bin");
  const builderLog = join(root, "builder.jsonl");
  const viteLog = join(root, "vite-ran");
  mkdirSync(scripts, { recursive: true });
  mkdirSync(bin);
  for (const name of ["package.mjs", "bundle-cli.mjs"]) {
    copyFileSync(join(sourceScripts, name), join(scripts, name));
  }
  const node = join(bin, process.platform === "win32" ? "node.exe" : "node");
  if (process.platform === "win32") copyFileSync(process.execPath, node);
  else symlinkSync(process.execPath, node);

  function tool(name, source) {
    if (process.platform === "win32") {
      writeFileSync(join(bin, `${name}.mjs`), source);
      writeFileSync(join(bin, `${name}.cmd`), `@"%~dp0node.exe" "%~dp0${name}.mjs" %*\r\n`);
    } else {
      writeFileSync(join(bin, name), `#!/usr/bin/env node\n${source}`, { mode: 0o755 });
    }
  }
  tool("electron-vite", `
    import { writeFileSync } from "node:fs";
    writeFileSync(process.env.TEST_VITE_LOG, "built");
  `);
  tool("electron-builder", `
    import { appendFileSync, existsSync, readdirSync, readFileSync } from "node:fs";
    import { join } from "node:path";
    const bin = join(process.cwd(), "resources", "bin");
    const files = existsSync(bin)
      ? Object.fromEntries(readdirSync(bin).map(name => [name, readFileSync(join(bin, name), "utf8")]))
      : {};
    appendFileSync(process.env.TEST_BUILDER_LOG, JSON.stringify({args: process.argv.slice(2), files}) + "\\n");
  `);

  // Keep Windows' shell settings, but exclude every host PATH spelling and Go.
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toUpperCase() !== "PATH"));
  Object.assign(env, { PATH: bin, TEST_BUILDER_LOG: builderLog, TEST_VITE_LOG: viteLog });
  const run = (script, args) => spawnSync(node, [join(scripts, script), ...args], {
    cwd: root, env, encoding: "utf8", timeout: 15_000,
  });
  return {
    resources,
    run,
    package: (...args) => run("package.mjs", [...args, "--publish", "never"]),
    prebuilt(goos, goarch, content) {
      const dir = join(root, "server", "bin", `${goos}-${goarch}`);
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, goos === "windows" ? "multica.exe" : "multica"), content);
    },
    builds: () => existsSync(builderLog)
      ? readFileSync(builderLog, "utf8").trim().split("\n").map(line => JSON.parse(line))
      : [],
    viteRan: () => existsSync(viteLog),
  };
}

describe("packaging requires the target CLI without Go", () => {
  it.each([
    ["--win", "--x64"],
    ["--linux", "--arm64"],
    ["--mac", "--x64"],
  ])("stops %s %s before electron-builder when the target is missing", (platform, arch) => {
    const f = fixture();
    f.prebuilt("windows", "arm64", "wrong target");
    mkdirSync(f.resources, { recursive: true });
    writeFileSync(join(f.resources, "multica"), "stale CLI");
    writeFileSync(join(f.resources, "multica.exe"), "stale Windows CLI");
    const result = f.package(platform, arch);
    expect(result.error).toBeUndefined();
    expect(f.viteRan()).toBe(true);
    expect(f.builds()).toEqual([]);
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("requires a bundled CLI");
  });

  it.each([
    ["--win", "--x64", "windows", "amd64", "multica.exe"],
    ["--linux", "--arm64", "linux", "arm64", "multica"],
    ["--mac", "--x64", "darwin", "amd64", "multica"],
  ])("packages %s %s with its prebuilt CLI", (platform, arch, goos, goarch, name) => {
    const f = fixture();
    f.prebuilt(goos, goarch, "target CLI");
    const result = f.package(platform, arch);
    expect(result.error).toBeUndefined();
    expect(result.status, result.stderr).toBe(0);
    expect(f.builds()).toEqual([{ args: expect.arrayContaining([platform, arch]), files: { [name]: "target CLI" } }]);
  });

  it("rejects an empty CLI before electron-builder", () => {
    const f = fixture();
    f.prebuilt("windows", "amd64", "");
    const result = f.package("--win", "--x64");
    expect(result.error).toBeUndefined();
    expect(f.builds()).toEqual([]);
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("requires a bundled CLI");
  });

  it("rechecks the CLI for each architecture in a release build", () => {
    const f = fixture();
    f.prebuilt("windows", "amd64", "x64 CLI");
    const result = f.package("--win", "--x64", "--arm64");
    expect(result.error).toBeUndefined();
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("requires a bundled CLI");
    expect(f.builds()).toEqual([{ args: expect.arrayContaining(["--win", "--x64"]), files: { "multica.exe": "x64 CLI" } }]);
  });

  it("keeps standalone CLI staging optional for frontend development", () => {
    const f = fixture();
    const result = f.run("bundle-cli.mjs", []);
    expect(result.error).toBeUndefined();
    expect(result.status, result.stderr).toBe(0);
    expect(result.stderr).toContain("auto-installing the latest release at runtime");
    expect(existsSync(f.resources)).toBe(false);
  });
});

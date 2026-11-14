// @vitest-environment node
import { spawnSync } from "node:child_process";
import {
  copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, beforeEach, expect, it } from "vitest";

let root, scripts, bin, marker;

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "multica-package-cli-"));
  scripts = join(root, "apps", "desktop", "scripts");
  bin = join(root, "bin");
  marker = join(root, "builder-args.txt");
  mkdirSync(scripts, { recursive: true });
  mkdirSync(bin);
  writeFileSync(marker, "");
  for (const script of ["bundle-cli.mjs", "package.mjs"]) {
    copyFileSync(fileURLToPath(new URL(script, import.meta.url)), join(scripts, script));
  }

  // Isolate PATH so no Go toolchain or real Electron packager is used.
  if (process.platform === "win32") {
    copyFileSync(process.execPath, join(bin, "node.exe"));
    writeFileSync(join(bin, "electron-vite.cmd"), "@exit /b 0\r\n");
    writeFileSync(join(bin, "electron-builder.cmd"), '@echo %*>>"%BUILDER_MARKER%"\r\n');
  } else {
    symlinkSync(process.execPath, join(bin, "node"));
    writeFileSync(join(bin, "electron-vite"), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
    writeFileSync(join(bin, "electron-builder"), '#!/bin/sh\necho "$*" >> "$BUILDER_MARKER"\n', { mode: 0o755 });
  }
});

afterEach(() => rmSync(root, { recursive: true, force: true }));

function runScript(name, args) {
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.toUpperCase() === "PATH") delete env[key];
  }
  const result = spawnSync(process.execPath, [join(scripts, name), ...args], {
    env: { ...env, PATH: bin, BUILDER_MARKER: marker },
    encoding: "utf8",
  });
  if (result.error) throw result.error;
  return result;
}

it("allows development without a CLI but refuses to package that target", () => {
  const dev = runScript("bundle-cli.mjs", ["--target-platform", "win32", "--target-arch", "x64"]);
  expect(dev.status).toBe(0);

  const result = runScript("package.mjs", ["--win", "--x64"]);
  expect(result.status).toBe(1);
  expect(result.stderr).toContain("Packaging Windows x64 requires a bundled CLI");
  expect(result.stderr).not.toContain("at main (");
  expect(readFileSync(marker, "utf8")).toBe("");
});

it("packages the available x64 CLI and stops before the missing arm64 target", () => {
  const prebuilt = join(root, "server", "bin", "windows-amd64");
  mkdirSync(prebuilt, { recursive: true });
  writeFileSync(join(prebuilt, "multica.exe"), "x64 fixture");

  const result = runScript("package.mjs", ["--win", "--x64", "--arm64"]);
  expect(result.status).toBe(1);
  expect(result.stderr).toContain("Packaging Windows arm64 requires a bundled CLI");
  const calls = readFileSync(marker, "utf8").trim().split(/\r?\n/);
  expect(calls).toHaveLength(1);
  expect(calls[0].split(" ")).toContain("--x64");
  expect(calls[0]).not.toContain("--arm64");
});

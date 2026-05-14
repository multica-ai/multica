#!/usr/bin/env node

import { mkdtemp, mkdir, readFile, rm, rename, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import process from "node:process";
import { spawn } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

const targetDir = process.argv[2];
const goos = process.argv[3];
const goarch = process.argv[4];

if (!targetDir || !goos || !goarch) {
  console.error("usage: prepare-claude-sdk-bundle.mjs <target-dir> <goos> <goarch>");
  process.exit(1);
}

const archMap = {
  amd64: "x64",
  arm64: "arm64",
};

const osMap = {
  darwin: "darwin",
  linux: "linux",
  windows: "win32",
};

const npmArch = archMap[goarch];
const npmOS = osMap[goos];

if (!npmArch || !npmOS) {
  console.error(`unsupported target: ${goos}/${goarch}`);
  process.exit(1);
}

function packageDirFromEntry(specifier) {
  const entry = require.resolve(specifier);
  let dir = path.dirname(entry);
  for (let i = 0; i < 4; i += 1) {
    const candidate = path.join(dir, "package.json");
    try {
      require(candidate);
      return dir;
    } catch {
      const parent = path.dirname(dir);
      if (parent === dir) break;
      dir = parent;
    }
  }
  throw new Error(`failed to resolve package dir for ${specifier}`);
}

async function readJSON(file) {
  return JSON.parse(await readFile(file, "utf8"));
}

function run(cmd, args, cwd) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, {
      cwd,
      env: process.env,
      stdio: "inherit",
    });
    child.on("exit", (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${cmd} ${args.join(" ")} exited with code ${code ?? "unknown"}`));
    });
    child.on("error", reject);
  });
}

function targetPlatformPackages(version) {
  if (goos === "linux") {
    return [
      { name: `@anthropic-ai/claude-agent-sdk-linux-${npmArch}@${version}`, extraArgs: ["--cpu", npmArch, "--os", npmOS, "--libc", "glibc"] },
      { name: `@anthropic-ai/claude-agent-sdk-linux-${npmArch}-musl@${version}`, extraArgs: ["--cpu", npmArch, "--os", npmOS, "--libc", "musl"] },
    ];
  }
  return [
    { name: `@anthropic-ai/claude-agent-sdk-${npmOS}-${npmArch}@${version}`, extraArgs: ["--cpu", npmArch, "--os", npmOS] },
  ];
}

async function main() {
  const sdkPkg = await readJSON(path.join(packageDirFromEntry("@anthropic-ai/claude-agent-sdk"), "package.json"));
  const zodPkg = await readJSON(path.join(packageDirFromEntry("zod"), "package.json"));

  const sdkVersion = sdkPkg.version;
  const zodVersion = zodPkg.version;

  const tempRoot = await mkdtemp(path.join(tmpdir(), "multica-claude-sdk-bundle-"));
  const workDir = path.join(tempRoot, "bundle");
  const stageDir = `${targetDir}.tmp`;
  await mkdir(workDir, { recursive: true });

  try {
    await writeFile(path.join(workDir, "package.json"), `${JSON.stringify({
      name: "multica-claude-sdk-runtime",
      private: true,
      type: "module",
      dependencies: {
        "@anthropic-ai/claude-agent-sdk": sdkVersion,
        zod: zodVersion,
      },
    }, null, 2)}\n`);

    await run("npm", [
      "install",
      "--omit=dev",
      "--omit=optional",
      "--ignore-scripts",
      "--no-package-lock",
    ], workDir);

    for (const pkg of targetPlatformPackages(sdkVersion)) {
      await run("npm", [
        "install",
        "--no-save",
        "--ignore-scripts",
        "--force",
        ...pkg.extraArgs,
        pkg.name,
      ], workDir);
    }

    await rm(path.join(workDir, "package-lock.json"), { force: true });
    await rm(stageDir, { recursive: true, force: true });
    await mkdir(path.dirname(targetDir), { recursive: true });
    await rename(workDir, stageDir);
    await rm(targetDir, { recursive: true, force: true });
    await rename(stageDir, targetDir);
  } finally {
    await rm(tempRoot, { recursive: true, force: true }).catch(() => {});
    await rm(stageDir, { recursive: true, force: true }).catch(() => {});
  }
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});

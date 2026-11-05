import {
  access,
  mkdir,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { join, dirname } from "node:path";
import { z } from "zod";
import { run } from "./process.js";
import { probeTool } from "./inspect.js";
import { problem, VerificationError } from "../host/problem.js";

async function exists(path: string) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}
async function download(
  url: string,
  path: string,
  expected: string,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(url, {
    redirect: "error",
    signal: signal
      ? AbortSignal.any([signal, AbortSignal.timeout(120000)])
      : AbortSignal.timeout(120000),
  });
  if (!response.ok || !response.body)
    throw new VerificationError(
      problem(
        "dependencyMissing",
        "The official toolchain download failed",
        "inspect",
      ),
    );
  const file = await import("node:fs/promises").then((fs) =>
    fs.open(path, "wx", 0o600),
  );
  const hash = createHash("sha256");
  let bytes = 0;
  try {
    for await (const chunk of response.body) {
      bytes += chunk.length;
      if (bytes > 512 * 1024 * 1024) throw new Error("Download limit");
      hash.update(chunk);
      await file.write(chunk);
    }
  } finally {
    await file.close();
  }
  if (hash.digest("hex") !== expected)
    throw new VerificationError(
      problem(
        "identityMismatch",
        "Downloaded toolchain checksum does not match the official manifest",
        "inspect",
      ),
    );
}

export async function prepareToolchain(
  checkout: string,
  root: string,
  signal?: AbortSignal,
): Promise<{ node: string; pnpm: string; env: NodeJS.ProcessEnv }> {
  const manifest = z
    .object({
      engines: z.object({ node: z.string() }),
      packageManager: z.string(),
    })
    .parse(JSON.parse(await readFile(join(checkout, "package.json"), "utf8")));
  const minimumNode = /^>=(\d+)/.exec(manifest.engines.node)?.[1];
  const pnpmVersion = /^pnpm@(\d+\.\d+\.\d+)(?:\+.*)?$/.exec(
    manifest.packageManager,
  )?.[1];
  if (!minimumNode || !pnpmVersion)
    throw new VerificationError(
      problem(
        "unsupportedCapability",
        "Unsupported checkout toolchain declaration",
        "inspect",
      ),
    );
  await mkdir(root, { recursive: true, mode: 0o700 });
  const log = join(root, "prepare.log");
  let node = process.execPath;
  if (Number(process.versions.node.split(".")[0]) < Number(minimumNode)) {
    if (process.platform !== "darwin")
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Portable toolchain installation currently supports macOS",
        ),
      );
    const releases = z
      .array(
        z.object({
          version: z.string(),
          lts: z.union([z.string(), z.boolean()]),
        }),
      )
      .parse(
        await (
          await fetch("https://nodejs.org/dist/index.json", {
            signal: AbortSignal.timeout(15000),
            redirect: "error",
          })
        ).json(),
      );
    const release = releases.find(
      (item) =>
        item.lts &&
        Number(item.version.slice(1).split(".")[0]) >= Number(minimumNode),
    );
    if (!release || !/^v\d+\.\d+\.\d+$/.test(release.version))
      throw new Error("No matching Node release");
    const name = `node-${release.version}-darwin-${process.arch === "arm64" ? "arm64" : "x64"}`;
    const directory = join(root, name);
    node = join(directory, "bin", "node");
    if (!(await exists(node))) {
      const sums = await (
        await fetch(
          `https://nodejs.org/dist/${release.version}/SHASUMS256.txt`,
          { signal: AbortSignal.timeout(15000), redirect: "error" },
        )
      ).text();
      const expected = sums
        .split("\n")
        .find((line) => line.endsWith(`  ${name}.tar.gz`))
        ?.split(/\s+/)[0];
      if (!expected || !/^[a-f0-9]{64}$/.test(expected))
        throw new Error("Invalid Node manifest");
      const temporary = join(root, `.node-${randomUUID()}`);
      await mkdir(temporary, { mode: 0o700 });
      try {
        const archive = join(temporary, "node.tar.gz");
        await download(
          `https://nodejs.org/dist/${release.version}/${name}.tar.gz`,
          archive,
          expected,
          signal,
        );
        await run("tar", ["-xzf", archive, "-C", temporary], {
          cwd: root,
          log,
          signal,
        });
        await rename(join(temporary, name), directory);
      } finally {
        await rm(temporary, { recursive: true, force: true });
      }
    }
  }
  const pnpmRoot = join(root, `pnpm-${pnpmVersion}`),
    pnpm = join(pnpmRoot, "node_modules", "pnpm", "bin", "pnpm.cjs");
  const env: NodeJS.ProcessEnv = {
    PATH: `${dirname(node)}:${process.env.PATH ?? ""}`,
    COREPACK_ENABLE_PROJECT_SPEC: "0",
    npm_config_manage_package_manager_versions: "false",
  };
  if (!(await exists(pnpm))) {
    await mkdir(pnpmRoot, { recursive: true, mode: 0o700 });
    await run(
      join(dirname(node), "npm"),
      [
        "install",
        "--prefix",
        pnpmRoot,
        "--ignore-scripts",
        "--no-audit",
        "--no-fund",
        `pnpm@${pnpmVersion}`,
      ],
      { cwd: root, env, log, signal },
    );
  }
  const goMod = await readFile(join(checkout, "server/go.mod"), "utf8");
  const goVersion = /^go\s+(\d+\.\d+(?:\.\d+)?)\s*$/m.exec(goMod)?.[1];
  if (!goVersion)
    throw new VerificationError(
      problem("invalidInput", "Missing checkout Go version"),
    );
  if (
    (await probeTool("go", ["version"], { cwd: root, signal })).kind ===
    "missing"
  ) {
    if (process.platform !== "darwin")
      throw new VerificationError(
        problem(
          "unsupportedCapability",
          "Portable Go installation currently supports macOS",
        ),
      );
    const destination = join(root, `go-${goVersion}`);
    if (!(await exists(join(destination, "bin", "go")))) {
      const list = z
        .array(
          z.object({
            version: z.string(),
            files: z.array(
              z.object({
                filename: z.string(),
                os: z.string(),
                arch: z.string(),
                sha256: z.string(),
              }),
            ),
          }),
        )
        .parse(
          await (
            await fetch("https://go.dev/dl/?mode=json&include=all", {
              signal: AbortSignal.timeout(15000),
              redirect: "error",
            })
          ).json(),
        );
      const item = list
        .find((entry) => entry.version === `go${goVersion}`)
        ?.files.find(
          (entry) =>
            entry.os === "darwin" &&
            entry.arch === (process.arch === "arm64" ? "arm64" : "amd64") &&
            entry.filename.endsWith(".tar.gz"),
        );
      if (
        !item ||
        !/^[a-f0-9]{64}$/.test(item.sha256) ||
        !/^go[\w.-]+\.tar\.gz$/.test(item.filename)
      )
        throw new Error("Missing Go archive");
      const temporary = join(root, `.go-${randomUUID()}`);
      await mkdir(temporary, { mode: 0o700 });
      try {
        const archive = join(temporary, "go.tar.gz");
        await download(
          `https://dl.google.com/go/${item.filename}`,
          archive,
          item.sha256,
          signal,
        );
        await run("tar", ["-xzf", archive, "-C", temporary], {
          cwd: root,
          log,
          signal,
        });
        await rename(join(temporary, "go"), destination);
      } finally {
        await rm(temporary, { recursive: true, force: true });
      }
    }
    env.PATH = `${join(destination, "bin")}:${env.PATH}`;
  }
  env.GOTOOLCHAIN = `go${goVersion}`;
  await writeFile(
    join(root, "selected.json"),
    JSON.stringify({ node, pnpm, goVersion }),
    { mode: 0o600 },
  );
  return { node, pnpm, env };
}

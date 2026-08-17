#!/usr/bin/env node
// Dev launcher for `pnpm dev:desktop`.
//
// Derives per-worktree isolation env (renderer port + app name) so multiple
// worktrees can run `pnpm dev:desktop` side-by-side, then runs the same chain
// as before — bundle the CLI, brand the dev Electron, start electron-vite —
// inheriting the augmented env. A plain `&&` chain in package.json can't do
// this: each `&&` step is its own process, so an env tweak in step 1 wouldn't
// reach electron-vite in step 3. Args (e.g. `--mode staging`) pass through to
// electron-vite.

import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { envWithLocalBins } from "./package.mjs";
import {
  applyWorktreeDevEnv,
  repoRootFromScriptDir,
} from "./worktree-dev-env.mjs";

const here = dirname(fileURLToPath(import.meta.url));

applyWorktreeDevEnv(process.env, {
  root: repoRootFromScriptDir(here),
  log: true,
});

function run(command, args, { shell = false, env = process.env } = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    env,
    shell,
  });
  if (result.error) {
    console.error(`[dev:desktop] failed to run ${command}: ${result.error.message}`);
    process.exit(1);
  }
  if (result.status !== 0) process.exit(result.status ?? 1);
}

const node = process.execPath;
run(node, [join(here, "bundle-cli.mjs")]);
run(node, [join(here, "brand-dev-electron.mjs")]);

const isWin = process.platform === "win32";
// electron-vite's bin lands in apps/desktop/node_modules/.bin under the
// isolated linker but only in the repo-root .bin under the hoisted linker
// (.npmrc node-linker=hoisted); envWithLocalBins puts both on PATH.
run("electron-vite", ["dev", ...process.argv.slice(2)], {
  shell: isWin,
  env: envWithLocalBins(process.env),
});

#!/usr/bin/env node
/**
 * Fail fast if `typescript` is not native 7+. Next, Cursor tsgo, and `tsc`
 * all resolve that package name — aliasing it to typescript6 silently puts
 * typecheck back on the JS compiler.
 */
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const pkgPath = require.resolve("typescript/package.json");
const pkg = require(pkgPath);
const major = Number.parseInt(pkg.version, 10);

if (pkg.name !== "typescript" || Number.isNaN(major) || major < 7) {
  console.error(
    `typecheck requires TypeScript >= 7.0.2; ${pkgPath} is ${pkg.name}@${pkg.version}`,
  );
  process.exit(1);
}

const tscJs = join(dirname(pkgPath), "lib", "tsc.js");
const tsc = spawnSync(process.execPath, [tscJs, "--version"], { encoding: "utf8" });
if (tsc.error) {
  console.error(`failed to spawn ${tscJs}: ${tsc.error.message}`);
  process.exit(1);
}
if (tsc.status !== 0) {
  console.error((tsc.stdout || tsc.stderr).trim() || `tsc --version exited ${tsc.status}`);
  process.exit(tsc.status ?? 1);
}
const versionLine = (tsc.stdout || tsc.stderr).trim();
if (!/^Version [7-9]\.|^Version [1-9]\d+\./.test(versionLine)) {
  console.error(`tsc is not TypeScript 7+: ${versionLine}`);
  process.exit(1);
}

console.log(`typescript ${pkg.version} (${pkgPath})`);
console.log(versionLine);

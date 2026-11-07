// Stages the cua-driver SDK for one macOS target into resources/native, which
// packaging ships outside app.asar: the SDK dlopens its native library by real path.
// pnpm installs only the host CPU's native package, so another target's
// package is fetched and checked against the lockfile integrity.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const value = (flag, fallback) => args.includes(flag) ? args[args.indexOf(flag) + 1] : fallback;
const platform = value("--target-platform", process.platform);
const arch = value("--target-arch", process.arch);
const output = resolve(root, "resources/native/cua-driver");

rmSync(output, { force: true, recursive: true });
if (platform === "darwin") {
  if (!["arm64", "x64"].includes(arch)) throw new Error("Computer Use requires an arm64 or x64 macOS target");
  // pnpm's virtual store keeps a package's dependencies beside it.
  const sdk = realpathSync(resolve(root, "node_modules/@trycua/cua-driver"));
  const scope = dirname(dirname(sdk));
  const { version } = JSON.parse(readFileSync(join(sdk, "package.json"), "utf8"));
  const modules = join(output, "node_modules");
  const copy = (from, name) => cpSync(from, join(modules, name), { recursive: true, dereference: true });
  copy(sdk, "@trycua/cua-driver");
  copy(realpathSync(join(scope, "@ubjs/core")), "@ubjs/core");
  copy(realpathSync(join(scope, "@ubjs/node")), "@ubjs/node");
  const native = `@trycua/cua-driver-darwin-${arch}`;
  const installed = join(scope, native);
  if (existsSync(installed)) copy(realpathSync(installed), native);
  else fetchPackage(native, version, join(modules, native));
}

function fetchPackage(name, version, destination) {
  const lock = readFileSync(resolve(root, "../../pnpm-lock.yaml"), "utf8");
  const entry = lock.indexOf(`'${name}@${version}':`);
  const integrity = entry < 0 ? undefined : /integrity: (sha512-[A-Za-z0-9+/=]+)/.exec(lock.slice(entry, entry + 400))?.[1];
  if (!integrity) throw new Error(`${name}@${version} is missing from pnpm-lock.yaml`);
  const temp = mkdtempSync(join(tmpdir(), "multica-cua-"));
  try {
    const file = execFileSync("npm", ["pack", `${name}@${version}`, "--silent", "--pack-destination", temp], { encoding: "utf8" }).trim().split("\n").pop();
    const tarball = join(temp, file);
    const actual = `sha512-${createHash("sha512").update(readFileSync(tarball)).digest("base64")}`;
    if (actual !== integrity) throw new Error(`${name}@${version} integrity mismatch`);
    mkdirSync(destination, { recursive: true });
    execFileSync("tar", ["-xzf", tarball, "-C", destination, "--strip-components=1"]);
  } finally {
    rmSync(temp, { force: true, recursive: true });
  }
}

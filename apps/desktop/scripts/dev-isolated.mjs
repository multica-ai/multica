#!/usr/bin/env node
import { execFileSync, spawn } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat, symlink } from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { createServer } from "node:net";
import { homedir, tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
  const port = server.address().port;
  await new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  return port;
}

const realHome = homedir();
const paths = [
  ".multica/desktop-runtime/launch.json",
  ".multica/desktop-runtime/daemon.pid",
  ".multica/desktop-runtime/cli",
  ".multica/profiles/desktop-runtime/config.json",
  ".codex/config.toml",
].map((path) => join(realHome, path));
async function installedState() {
  return Promise.all(paths.map(async (path) => {
    try {
      const [info, contents] = await Promise.all([stat(path), readFile(path)]);
      return `${info.mtimeMs}:${createHash("sha256").update(contents).digest("hex")}`;
    } catch (error) {
      if (error.code === "ENOENT") return null;
      throw error;
    }
  }));
}

const before = JSON.stringify(await installedState());
const isolatedHome = await mkdtemp(join(tmpdir(), "multica-isolated-"));
const toolsBin = join(isolatedHome, "tools");
await mkdir(toolsBin);
for (const name of ["node", "pnpm", "go"]) {
  const executable = name === "node" ? process.execPath : execFileSync("/usr/bin/which", [name], {encoding:"utf8"}).trim();
  await symlink(executable, join(toolsBin, name));
}
// Reuse compiler caches without exposing the user's provider executables or profile.
const goCaches = execFileSync("go", ["env", "GOCACHE", "GOMODCACHE"], {encoding:"utf8"}).trim().split("\n");
const env = {
  ...process.env,
  PATH: `${toolsBin}:/usr/bin:/bin:/usr/sbin:/sbin`,
  GOCACHE: goCaches[0],
  GOMODCACHE: goCaches[1],
  HOME: isolatedHome,
  USERPROFILE: isolatedHome,
  XDG_CONFIG_HOME: join(isolatedHome, ".config"),
  CODEX_HOME: join(isolatedHome, ".codex"),
  MULTICA_DEV_ISOLATED: "1",
  MULTICA_DEV_USER_DATA: join(isolatedHome, "desktop-user-data"),
  MULTICA_DEV_HEALTH_PORT: String(await freePort()),
  DESKTOP_RENDERER_PORT: String(await freePort()),
  DESKTOP_APP_SUFFIX: `isolated-${randomUUID().slice(0, 8)}`,
};
delete env.MULTICA_DAEMON_P2P_SERVER_URL;
for (const key of Object.keys(env)) {
  if (/^(ANTHROPIC_|OPENAI_|CLAUDE_|CODEX_)/.test(key) && key !== "CODEX_HOME") delete env[key];
}
const here = dirname(fileURLToPath(import.meta.url));
const child = spawn(process.execPath, [join(here, "dev.mjs"), ...process.argv.slice(2)], {
  env, stdio: "inherit", detached: process.platform !== "win32",
});
function stop(signal = "SIGTERM") {
  if (!child.pid) return;
  try { process.kill(process.platform === "win32" ? child.pid : -child.pid, signal); }
  catch (error) { if (error.code !== "ESRCH") throw error; }
}
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => stop(signal));
let verifying = false;
const guard = setInterval(async () => {
  if (verifying) return;
  verifying = true;
  try {
    if (JSON.stringify(await installedState()) !== before) {
      console.error("Installed runtime files changed; stopping this isolated process group.");
      stop();
    }
  } catch (error) { console.error(error); stop(); }
  finally { verifying = false; }
}, 1000);
child.once("error", (error) => { console.error(error); process.exitCode = 1; });
child.once("close", async (code) => {
  clearInterval(guard);
  stop();
  const unchanged = JSON.stringify(await installedState()) === before;
  if (unchanged) console.log("[isolated] Installed runtime and provider configuration unchanged.");
  else console.error("[isolated] Installed runtime verification failed.");
  await rm(isolatedHome, { recursive: true, force: true, maxRetries: 3 });
  process.exitCode = unchanged ? code ?? 1 : 1;
});

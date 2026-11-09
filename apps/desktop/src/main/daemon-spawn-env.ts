import { basename, delimiter, dirname, join } from "path";

// Lowercase `npm_*` / `pnpm_*` is what pnpm writes for a script run: its whole
// merged config (`npm_config_shamefully_hoist`, `npm_config_catalog`, ...) plus
// the running script's lifecycle state. Matched case-sensitively so uppercase
// user settings such as PNPM_HOME are kept.
const LAUNCHER_ENV_PREFIXES = ["npm_", "pnpm_"];

// The rest of the launcher env, named exactly because their prefixes also cover
// variables users set themselves (TURBO_TOKEN, ELECTRON_MIRROR, NODE_OPTIONS).
const LAUNCHER_ENV_NAMES = new Set([
  // pnpm's lifecycle and the bin shim that runs electron-vite.
  "NODE",
  "NODE_PATH",
  "INIT_CWD",
  "PNPM_SCRIPT_SRC_DIR",
  // turbo, per task.
  "TURBO_HASH",
  "TURBO_INVOCATION_DIR",
  "TURBO_IS_TUI",
  // electron-vite dev.
  "NODE_ENV",
  "NODE_ENV_ELECTRON_VITE",
  "ELECTRON_CLI_ARGS",
  "ELECTRON_ENTRY",
  "ELECTRON_EXEC_PATH",
  "ELECTRON_MAJOR_VER",
  "ELECTRON_RENDERER_URL",
]);

/**
 * Env for every CLI child the Desktop app spawns. The daemon passes its own env
 * on to each agent task, so whatever lands here is what agents see.
 *
 * A dev Desktop is launched by `pnpm dev:desktop` → turbo → `electron-vite
 * dev`, and each layer adds env describing that launch. Left in, pnpm in any
 * repo an agent works on reads the Multica workspace's config (hoisted
 * node_modules, its catalog), NODE_PATH and PATH resolve modules and bins from
 * the dev checkout, and NODE_ENV=development breaks Next.js and Vite
 * production builds. NODE_ENV_ELECTRON_VITE marks that launch, so the launcher
 * env is dropped only when it is present. A packaged app is not launched this
 * way, so its env reaches the daemon untouched.
 */
export function daemonSpawnEnv(env: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const spawnEnv: NodeJS.ProcessEnv = { ...env, MULTICA_LAUNCHED_BY: "desktop" };
  if (spawnEnv.NODE_ENV_ELECTRON_VITE === undefined) return spawnEnv;

  const scriptDir = spawnEnv.PNPM_SCRIPT_SRC_DIR;
  for (const name of Object.keys(spawnEnv)) {
    if (
      LAUNCHER_ENV_NAMES.has(name) ||
      LAUNCHER_ENV_PREFIXES.some((prefix) => name.startsWith(prefix))
    ) {
      delete spawnEnv[name];
    }
  }

  const pathKey = Object.keys(spawnEnv).find((k) => k.toUpperCase() === "PATH");
  if (pathKey && scriptDir) {
    spawnEnv[pathKey] = withoutScriptBins(spawnEnv[pathKey] ?? "", scriptDir);
  }
  return spawnEnv;
}

// pnpm puts `node_modules/.bin` of the script's package and each ancestor, and
// its own node-gyp shim, on PATH for the script. The login-shell PATH that
// fix-path recovers keeps them, after the user's own entries.
function withoutScriptBins(path: string, scriptDir: string): string {
  const scriptBins = new Set<string>();
  for (let dir = scriptDir; ; dir = dirname(dir)) {
    scriptBins.add(join(dir, "node_modules", ".bin"));
    if (dirname(dir) === dir) break;
  }
  return path
    .split(delimiter)
    .filter((entry) => !scriptBins.has(entry) && basename(entry) !== "node-gyp-bin")
    .join(delimiter);
}

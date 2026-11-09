/**
 * Env for every CLI child the Desktop app spawns. The daemon passes its own env
 * on to each agent task, so whatever lands here is what agents see.
 *
 * `electron-vite dev` overwrites NODE_ENV with "development" and sets
 * NODE_ENV_ELECTRON_VITE before launching Electron. Agents building a Next.js
 * or Vite app under that NODE_ENV fail at prerender, so both are dropped when
 * NODE_ENV_ELECTRON_VITE marks the value as electron-vite's. A packaged app is
 * not launched by electron-vite, so a NODE_ENV the user exported reaches the
 * daemon untouched.
 */
export function daemonSpawnEnv(env: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const spawnEnv: NodeJS.ProcessEnv = { ...env, MULTICA_LAUNCHED_BY: "desktop" };
  if (spawnEnv.NODE_ENV_ELECTRON_VITE !== undefined) {
    delete spawnEnv.NODE_ENV;
    delete spawnEnv.NODE_ENV_ELECTRON_VITE;
  }
  return spawnEnv;
}

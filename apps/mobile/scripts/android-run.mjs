import { spawnSync } from "node:child_process";

/**
 * Build and install the Android debug app, re-applying app.config.ts first.
 *
 * Expo only prebuilds when android/ is absent. Re-running it here is required
 * for app.config.ts changes (package, icon, permissions, and deep links) to
 * reach the generated Android project. android/ is generated and gitignored.
 */
run("pnpm", ["exec", "expo", "prebuild", "-p", "android", "--no-install"]);
run("pnpm", ["exec", "expo", "run:android", ...process.argv.slice(2)]);

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: process.platform === "win32",
    ...options,
  });

  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

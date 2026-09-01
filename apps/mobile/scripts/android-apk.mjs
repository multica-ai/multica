import { spawnSync } from "node:child_process";

/**
 * Produce an installable local Android debug APK without requiring an emulator.
 * The generated artifact is android/app/build/outputs/apk/debug/app-debug.apk.
 */
run("pnpm", ["exec", "expo", "prebuild", "-p", "android", "--no-install"]);
run(process.platform === "win32" ? "gradlew.bat" : "./gradlew", ["app:assembleDebug", ...process.argv.slice(2)], {
  cwd: new URL("../android/", import.meta.url),
});

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: process.platform === "win32",
    ...options,
  });

  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

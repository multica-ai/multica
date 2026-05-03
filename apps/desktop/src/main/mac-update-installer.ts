import { execFile, spawn } from "child_process";
import { existsSync } from "fs";
import { chmod, mkdtemp, readFile, writeFile } from "fs/promises";
import { tmpdir } from "os";
import { basename, dirname, join, sep } from "path";
import { promisify } from "util";

export const FURTHERREF_BUNDLE_ID = "com.furtherref.multica";

const execFileAsync = promisify(execFile);

export type MacInstallResult =
  | { ok: true; scriptPath: string; logPath: string }
  | { ok: false; error: string };

type QuitAwareApp = {
  once(event: "will-quit", handler: () => void): void;
};

type TimerHandle = ReturnType<typeof setTimeout>;

type ScheduleMacFallbackInstallOptions = {
  app: QuitAwareApp;
  delayMs: number;
  fallback: () => void;
  setTimer?: (handler: () => void, delayMs: number) => TimerHandle;
  clearTimer?: (timer: TimerHandle) => void;
};

type AppMetadata = {
  bundleId: string;
  version: string;
};

type InstallScriptOptions = {
  appBundlePath: string;
  backupBundlePath: string;
  newBundlePath: string;
  currentPid: number;
  logPath: string;
};

export function findContainingAppBundle(execPath: string): string | null {
  const parts = execPath.split(sep);
  for (let index = parts.length - 1; index >= 0; index--) {
    if (parts[index]?.endsWith(".app")) {
      return parts.slice(0, index + 1).join(sep) || sep;
    }
  }
  return null;
}

export function isVersionNewer(latest: string, current: string): boolean {
  const l = latest.replace(/^v/, "").split(".").map(Number);
  const c = current.replace(/^v/, "").split(".").map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = Number.isFinite(l[i]) ? l[i] : 0;
    const cv = Number.isFinite(c[i]) ? c[i] : 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

function shellQuote(value: string): string {
  return `"${value.replace(/(["\\$`])/g, "\\$1")}"`;
}

export function buildMacInstallScript(options: InstallScriptOptions): string {
  const appBundle = shellQuote(options.appBundlePath);
  const backupBundle = shellQuote(options.backupBundlePath);
  const newBundle = shellQuote(options.newBundlePath);
  const logPath = shellQuote(options.logPath);

  return `#!/bin/bash
set -euo pipefail

exec >> ${logPath} 2>&1
echo "[mac-update-installer] started at $(date)"

while kill -0 ${options.currentPid} >/dev/null 2>&1; do
  sleep 0.2
done

if [ ! -d ${newBundle} ]; then
  echo "[mac-update-installer] new app bundle is missing: ${options.newBundlePath}"
  exit 1
fi

rm -rf ${backupBundle}
mv ${appBundle} ${backupBundle}

if mv ${newBundle} ${appBundle}; then
  echo "[mac-update-installer] replacement succeeded"
  open ${appBundle}
  rm -rf ${backupBundle}
  exit 0
fi

echo "[mac-update-installer] replacement failed, restoring backup"
rm -rf ${appBundle}
mv ${backupBundle} ${appBundle}
open ${appBundle}
exit 1
`;
}

export function scheduleMacFallbackInstall({
  app,
  delayMs,
  fallback,
  setTimer = setTimeout,
  clearTimer = clearTimeout,
}: ScheduleMacFallbackInstallOptions): void {
  const timer = setTimer(fallback, delayMs);
  app.once("will-quit", () => {
    clearTimer(timer);
  });
}

async function readAppMetadata(appBundlePath: string): Promise<AppMetadata> {
  const plistPath = join(appBundlePath, "Contents", "Info.plist");
  const [{ stdout: bundleId }, { stdout: version }] = await Promise.all([
    execFileAsync("/usr/libexec/PlistBuddy", [
      "-c",
      "Print :CFBundleIdentifier",
      plistPath,
    ]),
    execFileAsync("/usr/libexec/PlistBuddy", [
      "-c",
      "Print :CFBundleShortVersionString",
      plistPath,
    ]),
  ]);

  return {
    bundleId: bundleId.trim(),
    version: version.trim(),
  };
}

async function extractUpdateZip(downloadedFile: string): Promise<string> {
  const workDir = await mkdtemp(join(tmpdir(), "multica-mac-update-"));
  await execFileAsync("/usr/bin/ditto", ["-x", "-k", downloadedFile, workDir]);
  return workDir;
}

async function findExtractedAppBundle(
  extractionDir: string,
  expectedAppName: string,
): Promise<string> {
  const candidate = join(extractionDir, expectedAppName);
  if (existsSync(candidate)) return candidate;

  throw new Error(`update archive did not contain ${expectedAppName}`);
}

async function verifyExtractedApp(
  currentAppBundlePath: string,
  newAppBundlePath: string,
): Promise<void> {
  const [currentMetadata, newMetadata] = await Promise.all([
    readAppMetadata(currentAppBundlePath),
    readAppMetadata(newAppBundlePath),
  ]);

  if (newMetadata.bundleId !== FURTHERREF_BUNDLE_ID) {
    throw new Error(
      `update bundle id mismatch: expected ${FURTHERREF_BUNDLE_ID}, got ${newMetadata.bundleId}`,
    );
  }

  if (!isVersionNewer(newMetadata.version, currentMetadata.version)) {
    throw new Error(
      `update version ${newMetadata.version} is not newer than current version ${currentMetadata.version}`,
    );
  }

  await execFileAsync("/usr/bin/codesign", [
    "--verify",
    "--deep",
    "--strict",
    newAppBundlePath,
  ]);
}

export async function customInstallMacApp(
  downloadedFile: string,
  execPath = process.execPath,
  currentPid = process.pid,
): Promise<MacInstallResult> {
  try {
    if (process.platform !== "darwin") {
      return { ok: false, error: "custom mac installer is darwin-only" };
    }

    const appBundlePath = findContainingAppBundle(execPath);
    if (!appBundlePath) {
      return {
        ok: false,
        error: `cannot locate containing .app bundle from ${execPath}`,
      };
    }

    const extractionDir = await extractUpdateZip(downloadedFile);
    const newBundlePath = await findExtractedAppBundle(
      extractionDir,
      basename(appBundlePath),
    );

    await verifyExtractedApp(appBundlePath, newBundlePath);

    const stamp = `${Date.now()}`;
    const backupBundlePath = join(
      dirname(appBundlePath),
      `${basename(appBundlePath)}.backup-${stamp}`,
    );
    const logPath = join(extractionDir, "install.log");
    const scriptPath = join(extractionDir, "install.sh");
    const script = buildMacInstallScript({
      appBundlePath,
      backupBundlePath,
      newBundlePath,
      currentPid,
      logPath,
    });

    await writeFile(scriptPath, script, "utf-8");
    await chmod(scriptPath, 0o755);

    const child = spawn("/bin/bash", [scriptPath], {
      detached: true,
      stdio: "ignore",
    });
    child.unref();

    return { ok: true, scriptPath, logPath };
  } catch (err) {
    return {
      ok: false,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

export async function readInstallerLog(logPath: string): Promise<string> {
  return readFile(logPath, "utf-8");
}

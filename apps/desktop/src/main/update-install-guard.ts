import { execFile } from "node:child_process";
import { win32 } from "node:path";
import { bundledCliPath } from "./bundled-cli";
import type { UpdateInstallCheck, UpdateInstallDiagnostic } from "../shared/updater-types";

// A running bundled CLI holds its executable open on Windows. Never stop it
// for an update: it may own active runs, even when Desktop is closing. Query
// only process metadata; no credentials, process termination, or user scripts.
const PROCESS_CHECK = [
  "$ErrorActionPreference = 'Stop'",
  "$cliPath = [IO.Path]::GetFullPath($env:MULTICA_UPDATE_CLI_PATH)",
  "$blocked = @(Get-CimInstance Win32_Process -Filter \"Name = 'multica.exe'\" | Where-Object {",
  "  -not $_.ExecutablePath -or [IO.Path]::GetFullPath($_.ExecutablePath) -ieq $cliPath",
  "}).Count -gt 0",
  "if ($blocked) { 'blocked' } else { 'clear' }",
].join("\n");

function failed(diagnostic: UpdateInstallDiagnostic): UpdateInstallCheck {
  // Only bounded codes: never log paths, environment, process details or stderr.
  console.warn(`[updater] install deferred: ${diagnostic}`);
  return { allowed: false, reason: "probe_failed", diagnostic };
}

export async function checkWindowsUpdateInstall(platform = process.platform): Promise<UpdateInstallCheck> {
  if (platform !== "win32") return { allowed: true };
  const systemRoot = process.env.SystemRoot;
  if (!systemRoot || !win32.isAbsolute(systemRoot)) return failed("system_root_missing");
  return new Promise((resolve) => {
    execFile(
      win32.join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
      ["-NoLogo", "-NoProfile", "-NonInteractive", "-Command", PROCESS_CHECK],
      {
        windowsHide: true,
        timeout: 6_000,
        maxBuffer: 4_096,
        env: { ...process.env, MULTICA_UPDATE_CLI_PATH: bundledCliPath() },
      },
      (error, stdout) => {
        if (error) {
          resolve(failed(error.killed ? "timed_out" : "launch_failed"));
        } else if (stdout.trim() === "clear") {
          resolve({ allowed: true });
        } else if (stdout.trim() === "blocked") {
          resolve({ allowed: false, reason: "runtime_running" });
        } else {
          resolve(failed("invalid_output"));
        }
      },
    );
  });
}

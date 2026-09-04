export interface UpdaterPreferences {
  automaticUpdates: boolean;
}

export type UpdateInstallDiagnostic =
  | "system_root_missing"
  | "launch_failed"
  | "timed_out"
  | "invalid_output"
  | "probe_failed";

export type UpdateInstallCheck =
  | { allowed: true }
  | { allowed: false; reason: "runtime_running" }
  | { allowed: false; reason: "probe_failed"; diagnostic: UpdateInstallDiagnostic };

export type UpdateInstallState =
  | { status: "idle" }
  | { status: "ready" | "checking"; version: string }
  | ({ status: "deferred"; version: string } & Exclude<UpdateInstallCheck, { allowed: true }>);

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
    }
  | { ok: false; error: string };

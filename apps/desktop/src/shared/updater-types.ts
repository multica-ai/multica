export interface UpdaterPreferences {
  automaticUpdates: boolean;
}

export type UpdaterFailureReason = "unavailable" | "network" | "unknown";

export type UpdaterDownloadState =
  | { status: "idle" }
  | { status: "downloading"; version: string }
  | { status: "downloaded"; version: string }
  | { status: "error"; reason: UpdaterFailureReason };

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
      downloadState: UpdaterDownloadState;
    }
  | { ok: false; error: string; reason: UpdaterFailureReason };

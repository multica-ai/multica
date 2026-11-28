export interface UpdaterPreferences {
  automaticUpdates: boolean;
}

// "not-configured": the build was packaged for a self-hosted server without an
// update feed (MULTICA_DESKTOP_SERVER_URL without MULTICA_DESKTOP_UPDATE_URL).
export type UpdaterFailureReason =
  | "unavailable"
  | "network"
  | "unknown"
  | "not-configured";

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

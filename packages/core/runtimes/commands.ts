export const CLOUD_SERVER_URL = "https://api.multica.ai";
export const CLOUD_APP_URL = "https://multica.ai";

export function normalizeCommandURL(url: string | undefined): string {
  return url?.trim().replace(/\/+$/, "") ?? "";
}

export type DaemonSetupTarget = "cloud" | "self_host" | "unknown";
export type DaemonConfigStatus = "loading" | "ready" | "error";

export interface DaemonSetupConfig {
  status: DaemonConfigStatus;
  target: DaemonSetupTarget;
  serverUrl?: string;
  appUrl?: string;
}

export type DaemonCommands =
  | { status: "ready"; setupCmd: string; tokenCmd: string }
  | { status: "loading" | "error" | "unknown" | "incomplete" };

export function daemonCommands(config: DaemonSetupConfig): DaemonCommands {
  if (config.status !== "ready") return { status: config.status };

  if (config.target === "self_host") {
    const normalizedServerUrl = normalizeCommandURL(config.serverUrl);
    const normalizedAppUrl = normalizeCommandURL(config.appUrl);
    if (!normalizedServerUrl || !normalizedAppUrl) return { status: "incomplete" };
    return {
      status: "ready",
      setupCmd: `multica setup self-host --server-url ${normalizedServerUrl} --app-url ${normalizedAppUrl}`,
      tokenCmd: `multica config set server_url ${normalizedServerUrl}
multica config set app_url ${normalizedAppUrl}
multica login --token <YOUR_TOKEN>
multica daemon start`,
    };
  }

  // Empty URLs are also returned by misconfigured self-hosted servers.
  if (config.target !== "cloud") return { status: "unknown" };
  return {
    status: "ready",
    setupCmd: "multica setup",
    tokenCmd: `multica config set server_url ${CLOUD_SERVER_URL}
multica config set app_url ${CLOUD_APP_URL}
multica login --token <YOUR_TOKEN>
multica daemon start`,
  };
}

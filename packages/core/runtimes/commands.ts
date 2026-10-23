export const CLOUD_SERVER_URL = "https://api.multica.ai";
export const CLOUD_APP_URL = "https://multica.ai";

export function normalizeCommandURL(url: string | undefined): string {
  return url?.trim().replace(/\/+$/, "") ?? "";
}

export interface DaemonCommands {
  setupCmd: string;
  tokenCmd: string;
}

export function daemonCommands(
  serverUrl: string | undefined,
  appUrl: string | undefined,
): DaemonCommands {
  const normalizedServerUrl = normalizeCommandURL(serverUrl);
  const normalizedAppUrl = normalizeCommandURL(appUrl);
  if (normalizedServerUrl && normalizedAppUrl) {
    return {
      setupCmd: `multica setup self-host --server-url ${normalizedServerUrl} --app-url ${normalizedAppUrl}`,
      tokenCmd: `multica config set server_url ${normalizedServerUrl}
multica config set app_url ${normalizedAppUrl}
multica login --token <YOUR_TOKEN>
multica daemon start`,
    };
  }

  return {
    setupCmd: "multica setup",
    tokenCmd: `multica config set server_url ${CLOUD_SERVER_URL}
multica config set app_url ${CLOUD_APP_URL}
multica login --token <YOUR_TOKEN>
multica daemon start`,
  };
}

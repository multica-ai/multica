/**
 * Canonical CLI install + daemon setup commands shown to users.
 *
 * Both the Runtimes "Add a computer" dialog and the onboarding "Connect
 * via terminal" instructions must render the SAME command, and it must be
 * environment-aware: on a self-hosted deployment the second step is
 * `multica setup self-host --server-url <api> --app-url <app>` derived from
 * the server's `/api/config` (`daemon_server_url` / `daemon_app_url`), not
 * the cloud `multica setup`. Hardcoding `multica setup` here sent self-host
 * users to Multica Cloud (api.multica.ai).
 */

const CLOUD_SERVER_URL = "https://api.multica.ai";
const CLOUD_APP_URL = "https://multica.ai";

export const INSTALL_CMD =
  "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash";

export interface DaemonSetupCommands {
  /** Step 2: configure + authenticate + start the daemon, in one command. */
  setupCmd: string;
  /** Manual fallback used when the browser cannot reach the CLI. */
  tokenCmd: string;
}

export function normalizeCommandURL(url: string | undefined) {
  return url?.trim().replace(/\/+$/, "") ?? "";
}

/**
 * Build the setup commands for the given deployment. When both the server
 * (API) and app (frontend) URLs are known, emit the self-host form; otherwise
 * fall back to the cloud `multica setup`.
 */
export function daemonSetupCommands(
  serverUrl: string | undefined,
  appUrl: string | undefined,
): DaemonSetupCommands {
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

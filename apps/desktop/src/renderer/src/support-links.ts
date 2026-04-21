export const WEB_APP_URL =
  import.meta.env.VITE_APP_URL || "http://localhost:3000";

export const DESKTOP_BROWSER_LOGIN_URL = `${WEB_APP_URL}/login?platform=desktop`;

export const CLI_INSTALLATION_GUIDE_URL =
  "https://github.com/multica-ai/multica#cli-installation";

export const CLI_AND_DAEMON_GUIDE_URL =
  "https://github.com/multica-ai/multica/blob/main/CLI_AND_DAEMON.md";

export const DESKTOP_SANDBOX_TROUBLESHOOTING_URL =
  "https://github.com/multica-ai/multica/blob/main/docs/codex-sandbox-troubleshooting.md";

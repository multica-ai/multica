export type ConnectorPlatform = "unix" | "windows";

export const CONNECTOR_DEPLOYMENT_URL = "https://multica.fluma.ai:26081";

export const CONNECTOR_INSTALL_COMMANDS: Record<ConnectorPlatform, string> = {
  unix: "curl -fsSL https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.sh | bash",
  windows:
    "irm https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.ps1 | iex",
};

export const CONNECTOR_SETUP_COMMAND = "multica setup";

export const CONNECTOR_TOKEN_COMMAND = `multica config set server_url ${CONNECTOR_DEPLOYMENT_URL}
multica config set app_url ${CONNECTOR_DEPLOYMENT_URL}
multica login --token <YOUR_TOKEN>
multica daemon start`;

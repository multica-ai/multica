const DEFAULT_WORKSPACE_URL_HOST = "multica.ai";

let workspaceUrlHost: string = DEFAULT_WORKSPACE_URL_HOST;

export function getWorkspaceUrlHost(): string {
  return workspaceUrlHost;
}

export function setWorkspaceUrlHost(host: string | undefined): void {
  workspaceUrlHost = host && host.length > 0 ? host : DEFAULT_WORKSPACE_URL_HOST;
}

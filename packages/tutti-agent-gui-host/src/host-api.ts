import type { AgentHostInputApi } from "@tutti-os/agent-gui";
import type { AgentGuiInitSnapshot, AgentGuiRpcClient, AgentGuiWorkspaceState } from "./protocol";
import { localGuiProjects } from "./local-projects";

export function createAgentGuiHostApi(
  client: AgentGuiRpcClient,
  snapshot: Pick<AgentGuiInitSnapshot, "workspaceId" | "locale" | "local">,
  workspaceSource: {
    getWorkspaceState(): AgentGuiWorkspaceState;
    subscribeWorkspace(listener: () => void): () => void;
  },
  sessionId?: string | null,
): AgentHostInputApi {
  const unsupported = async (): Promise<never> => {
    throw new Error(snapshot.locale === "zh-CN"
      ? "当前运行环境不支持访问本机文件，请通过 Multica 任务提供文件"
      : "Local file access is unavailable in this host. Provide files through a Multica task.");
  };
  return {
    meta: { workspaceId: snapshot.workspaceId },
    ...(snapshot.local ? { userProjects: {
      list: async () => ({ projects: localGuiProjects(workspaceSource.getWorkspaceState()) }),
      subscribe: workspaceSource.subscribeWorkspace,
      use: async ({ path }: { path: string }) => {
        const project = localGuiProjects(workspaceSource.getWorkspaceState()).find((entry) => entry.path === path);
        if (!project) throw new Error("Local project directory not found in this workspace");
        return project;
      },
      pin: async () => { throw new Error("Manage local projects from the conversation list"); },
    } } : {}),
    onHostEvent(listener) {
      const settings = () => new Map(workspaceSource.getWorkspaceState().agents.map(({ agent }) => [agent.id, JSON.stringify(agent.runtime_config)]));
      let previous = settings();
      return workspaceSource.subscribeWorkspace(() => {
        const next = settings();
        for (const [agentTargetId, configuration] of next) {
          if (configuration !== previous.get(agentTargetId)) {
            listener({ type: "agent-composer-defaults-invalidated", scope: "global", agentTargetId });
          }
        }
        previous = next;
      });
    },
    clipboard: {
      writeText: (text) => navigator.clipboard.writeText(text),
    },
    filesystem: { readFileText: unsupported },
    workspace: {
      ensureDirectory: unsupported,
      readFile: snapshot.local && sessionId ? async ({ path }) => {
        const { url } = await client.request("image.url", { sessionId, path });
        const response = await fetch(url);
        if (!response.ok) throw new Error(`Local file request failed (HTTP ${response.status})`);
        return { path, bytes: new Uint8Array(await response.arrayBuffer()) };
      } : unsupported,
      selectDirectory: unsupported,
      selectFiles: unsupported,
      writeFileText: unsupported,
    },
    toast: {
      error: (message) => { void client.request("host.showMessage", { message, tone: "error" }); },
      info: (message) => { void client.request("host.showMessage", { message, tone: "info" }); },
      success: (message) => { void client.request("host.showMessage", { message, tone: "success" }); },
    },
  };
}

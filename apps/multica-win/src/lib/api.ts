import { invoke } from '@tauri-apps/api/core';

export interface AppConfig {
  server_url: string;
  app_url?: string;
  token?: string;
  workspace_id?: string;
  watched_workspaces?: Array<{ id: string; name: string }>;
}

export interface DaemonStatus {
  running: boolean;
  pid?: number;
  log_lines: string[];
  error?: string;
}

async function safeInvoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T | null> {
  try {
    return await invoke<T>(cmd, args);
  } catch {
    return null;
  }
}

export const api = {
  getConfig: () => safeInvoke<AppConfig>('get_config'),
  saveConfig: (config: AppConfig) => invoke<void>('save_config', { config }),

  checkHealth: () => safeInvoke<Record<string, unknown>>('check_health'),
  getCurrentUser: () => safeInvoke<Record<string, unknown>>('get_current_user'),
  getWorkspaces: () => safeInvoke<Array<Record<string, unknown>>>('get_workspaces'),
  getRuntimes: (wsId: string) => safeInvoke<Array<Record<string, unknown>>>('get_runtimes', { workspaceId: wsId }),
  getAgents: (wsId: string) => safeInvoke<Array<Record<string, unknown>>>('get_agents', { workspaceId: wsId }),
  getIssues: (wsId: string) => safeInvoke<Record<string, unknown>>('get_issues', { workspaceId: wsId }),
  getInbox: (wsId: string) => safeInvoke<Array<Record<string, unknown>>>('get_inbox', { workspaceId: wsId }),
  getTokenUsage: (wsId: string) => safeInvoke<Record<string, unknown>>('get_token_usage', { workspaceId: wsId }),

  startDaemon: (): Promise<string> => invoke('start_daemon'),
  stopDaemon: (): Promise<string> => invoke('stop_daemon'),
  daemonStatus: (): Promise<DaemonStatus> => invoke('daemon_status'),
  openFolder: (path: string): Promise<void> => invoke('cmd_open_folder', { path }),
  quitApp: (): Promise<void> => invoke('quit_app'),
};

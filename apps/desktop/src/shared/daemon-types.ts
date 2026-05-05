export type DaemonState =
  | "running"
  | "stopped"
  | "starting"
  | "stopping"
  | "installing_cli"
  | "cli_not_found";

export interface DaemonStatus {
  state: DaemonState;
  pid?: number;
  uptime?: string;
  daemonId?: string;
  deviceName?: string;
  agents?: string[];
  workspaceCount?: number;
  /** CLI profile this daemon belongs to. Empty string means the default profile. */
  profile?: string;
  /** Backend URL the daemon connects to. */
  serverUrl?: string;
}

export interface DaemonPrefs {
  autoStart: boolean;
  autoStop: boolean;
}

export type DaemonErrorCode =
  | "operation_in_progress"
  | "cli_not_installed"
  | "log_file_missing";

export type DaemonActionResult = {
  success: boolean;
  error?: string;
  errorCode?: DaemonErrorCode;
};

export const DAEMON_STATE_COLORS: Record<DaemonState, string> = {
  running: "bg-emerald-500",
  stopped: "bg-muted-foreground/40",
  starting: "bg-amber-500 animate-pulse",
  stopping: "bg-amber-500 animate-pulse",
  installing_cli: "bg-sky-500 animate-pulse",
  cli_not_found: "bg-red-500",
};

/**
 * Returns i18n dictionary keys for each daemon state. The consuming component
 * is responsible for calling its `t()` translator with these keys.
 */
export function getDaemonStateKeys(state: DaemonState): string {
  switch (state) {
    case "running":
      return "daemon_running";
    case "stopped":
      return "daemon_stopped";
    case "starting":
      return "daemon_starting";
    case "stopping":
      return "daemon_stopping";
    case "installing_cli":
      return "daemon_installing";
    case "cli_not_found":
      return "daemon_setup_failed";
  }
}

/**
 * Legacy English-only labels kept for backward compatibility.
 * Prefer getDaemonStateKeys() + t() in React components.
 */
export const DAEMON_STATE_LABELS: Record<DaemonState, string> = {
  running: "运行中",
  stopped: "已停止",
  starting: "启动中...",
  stopping: "停止中...",
  installing_cli: "设置中...",
  cli_not_found: "设置失败",
};

export function formatUptime(uptime?: string): string {
  if (!uptime) return "";
  const match = uptime.match(/(?:(\d+)h)?(\d+)m/);
  if (!match) return uptime;
  const h = match[1] ? `${match[1]}h ` : "";
  const m = match[2] ? `${match[2]}m` : "";
  return `${h}${m}`.trim() || uptime;
}

/**
 * Returns i18n key and optional params for the daemon state description.
 * The consuming component calls t(key, params) to get the localized string.
 *
 * `runtimeCount` is the number of runtimes the local daemon has registered
 * (claude / codex / gemini / ... — one per detected CLI). It's only consulted
 * when state === "running".
 */
export function daemonStateDescKey(
  state: DaemonState,
  runtimeCount: number,
): { key: string; params?: Record<string, string | number> } {
  switch (state) {
    case "running":
      if (runtimeCount === 0) {
        return { key: "daemon_desc_running_zero" };
      }
      if (runtimeCount === 1) {
        return { key: "daemon_desc_running_single" };
      }
      return { key: "daemon_desc_running_plural", params: { count: runtimeCount } };
    case "stopped":
      return { key: "daemon_desc_stopped" };
    case "starting":
      return { key: "daemon_desc_starting" };
    case "stopping":
      return { key: "daemon_desc_stopping" };
    case "installing_cli":
      return { key: "daemon_desc_installing" };
    case "cli_not_found":
      return { key: "daemon_desc_setup_failed" };
  }
}

/**
 * Legacy English-only description. Prefer daemonStateDescKey() + t() in React components.
 */
export function daemonStateDescription(state: DaemonState, runtimeCount: number): string {
  switch (state) {
    case "running":
      if (runtimeCount === 0) {
        return "运行中，但尚无运行时注册。";
      }
      if (runtimeCount === 1) {
        return "运行中 · 1 个运行时可用。";
      }
      return `运行中 · ${runtimeCount} 个运行时可用。`;
    case "stopped":
      return "未运行 · 此设备无法接受新任务。";
    case "starting":
      return "正在启动本地守护进程...";
    case "stopping":
      return "正在关闭本地守护进程...";
    case "installing_cli":
      return "首次设置运行时。只需一次。";
    case "cli_not_found":
      return "设置失败 · 无法下载运行时。请检查网络。";
  }
}

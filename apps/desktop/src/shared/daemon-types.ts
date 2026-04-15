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

export const DAEMON_STATE_COLORS: Record<DaemonState, string> = {
  running: "bg-emerald-500",
  stopped: "bg-muted-foreground/40",
  starting: "bg-amber-500 animate-pulse",
  stopping: "bg-amber-500 animate-pulse",
  installing_cli: "bg-sky-500 animate-pulse",
  cli_not_found: "bg-red-500",
};

export const DAEMON_STATE_LABELS: Record<DaemonState, string> = {
  running: "Running",
  stopped: "Stopped",
  starting: "Starting…",
  stopping: "Stopping…",
  installing_cli: "Setting up…",
  cli_not_found: "Setup Failed",
};

export function formatUptime(uptime?: string): string {
  if (!uptime) return "";
  const match = uptime.match(/(?:(\d+)h)?(\d+)m/);
  if (!match) return uptime;
  const h = match[1] ? `${match[1]}h ` : "";
  const m = match[2] ? `${match[2]}m` : "";
  return `${h}${m}`.trim() || uptime;
}

/** Where an environment came from: declared in `.multica/environments.toml`
 *  and started by the daemon, or a server the daemon found listening inside
 *  a task directory. */
export type IssueEnvironmentSource = "config" | "detected";

/** A `server` opens at its URL; an `app` is a desktop app with no URL, whose
 *  window only Desktop on the machine running it can bring forward. */
export type IssueEnvironmentKind = "server" | "app";

/** `unreachable` is derived by the server when the daemon that reported a
 *  live environment has gone quiet. */
export type IssueEnvironmentState = "starting" | "running" | "exited" | "stopped" | "unreachable";

export type IssueEnvironmentAction = "stop" | "restart" | "start" | "logs";

/** `pending` covers both queued and claimed; `expired` means the machine never
 *  answered in time. */
export type IssueEnvironmentActionStatus = "pending" | "succeeded" | "failed" | "expired";

/** A stop, restart, start or log read a member asked a machine to perform. */
export interface IssueEnvironmentActionRecord {
  id: string;
  action: IssueEnvironmentAction;
  status: IssueEnvironmentActionStatus;
  error: string;
  environment_id: string | null;
  runtime_id: string;
  repo: string;
  name: string;
  actor_type: string;
  actor_id: string | null;
  created_at: string;
  completed_at: string | null;
}

export interface IssueEnvironmentCommand extends IssueEnvironmentActionRecord {
  /** The log a `logs` command read, once it succeeded. */
  output: string | null;
}

export interface IssueEnvironment {
  id: string;
  issue_id: string;
  task_id: string;
  runtime_id: string | null;
  agent_id: string | null;
  /** The reporting daemon's own name for the environment. */
  local_key: string;
  source: IssueEnvironmentSource;
  kind: IssueEnvironmentKind;
  name: string;
  label: string;
  url: string;
  port: number | null;
  state: IssueEnvironmentState;
  detail: string;
  /** The last lines a crashed environment printed, redacted by the server. */
  excerpt: string;
  started_at: string;
  expires_at: string | null;
  last_seen_at: string;
  ended_at: string | null;
  /** Reported by a daemon that carries out sidebar commands. */
  controllable: boolean;
  /** The caller may stop or restart it. */
  can_control: boolean;
  pending_action: IssueEnvironmentActionRecord | null;
  last_action: IssueEnvironmentActionRecord | null;
  /** The sidebar start or restart that created this environment. */
  origin: IssueEnvironmentActionRecord | null;
}

/** One environment a repository declares, as the start menu offers it. */
export interface DeclaredEnvironment {
  repo: string;
  name: string;
  label: string;
  start: string;
}

/** What the latest run on one machine declared. */
export interface IssueEnvironmentConfig {
  runtime_id: string;
  task_id: string;
  reported_at: string;
  can_control: boolean;
  environments: DeclaredEnvironment[];
}

export interface IssueEnvironmentsResponse {
  environments: IssueEnvironment[];
  configs: IssueEnvironmentConfig[];
  /** Sidebar starts whose environment has not been reported yet. */
  pending_starts: IssueEnvironmentActionRecord[];
}

export interface StartIssueEnvironmentRequest {
  task_id: string;
  repo: string;
  name: string;
}

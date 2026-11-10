/** Where an environment came from: declared in `.multica/environments.toml`
 *  and started by the daemon, or a server the daemon found listening inside
 *  a task directory. */
export type IssueEnvironmentSource = "config" | "detected";

/** `unreachable` is derived by the server when the daemon that reported a
 *  live environment has gone quiet. */
export type IssueEnvironmentState = "starting" | "running" | "exited" | "stopped" | "unreachable";

export interface IssueEnvironment {
  id: string;
  issue_id: string;
  task_id: string;
  runtime_id: string | null;
  agent_id: string | null;
  source: IssueEnvironmentSource;
  name: string;
  label: string;
  url: string;
  port: number | null;
  state: IssueEnvironmentState;
  detail: string;
  started_at: string;
  expires_at: string | null;
  last_seen_at: string;
  ended_at: string | null;
}

export interface IssueEnvironmentsResponse {
  environments: IssueEnvironment[];
}

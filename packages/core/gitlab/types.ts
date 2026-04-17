export interface GitlabConnection {
  workspace_id: string;
  gitlab_project_id: number;
  gitlab_project_path: string;
  service_token_user_id: number;
  service_token_username?: string;
  connection_status: "connecting" | "connected" | "error";
  status_message?: string;
}

export interface ConnectGitlabInput {
  project: string; // numeric id or "group/project"
  token: string;
}

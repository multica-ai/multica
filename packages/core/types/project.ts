export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectPriority = "urgent" | "high" | "medium" | "low" | "none";

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead_type: "member" | "agent" | null;
  lead_id: string | null;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
  resource_count: number;
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent";
  lead_id?: string;
  // Resources to attach in the same transaction as the project. Server returns
  // 4xx (and rolls back) if any one is invalid or duplicate.
  resources?: CreateProjectResourceRequest[];
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent" | null;
  lead_id?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

export type ProjectBindingStatus = "bound" | "missing" | "stale" | "unauthorized";

export type ProjectAictxState =
  | "unconfigured"
  | "ready"
  | "stale"
  | "generating"
  | "preview_required"
  | "applying"
  | "blocked"
  | "provider_unavailable"
  | "permission_denied"
  | "error";

export type ProjectAictxContextIndexStatus = "ready" | "missing" | "stale" | "unknown";

export type ProjectAictxRedactionStatus = "passed" | "not_needed" | "failed";

export interface ProjectBinding {
  schema_version: number;
  contract_name: string;
  binding_id: string | null;
  workspace_id: string;
  multica_project_id: string;
  project_resource_id: string | null;
  repo_root_ref_redacted: string | null;
  repo_root_sha256: string | null;
  binding_source: string | null;
  verified_at: string | null;
  verified_by: string | null;
  symlink_policy: string;
  status: ProjectBindingStatus;
  reason_codes: string[];
}

export interface ProjectAictxStatus {
  schema_version: number;
  contract_name: string;
  workspace_id: string;
  multica_project_id: string;
  project_binding_id: string | null;
  state: ProjectAictxState;
  context_index_status: ProjectAictxContextIndexStatus;
  binding: ProjectBinding;
  latest_context_pack_ref: string | null;
  latest_context_pack_sha256: string | null;
  latest_context_pack_created_at: string | null;
  latest_handoff_ref: string | null;
  latest_decision_ref: string | null;
  redaction_status: ProjectAictxRedactionStatus;
  redaction_report_id: string | null;
  audit_event_id: string | null;
  reason_codes: string[];
}

// ProjectResource is a typed pointer from a project to an external resource.
// The resource_ref shape depends on resource_type. New types add a case in
// validateAndNormalizeResourceRef on the server and a renderer in the UI.
//
// Known types (UI must default-case unknown server-side additions):
//   - github_repo: cloud-side git checkout, ref = { url, default_branch_hint? }
//   - local_directory: in-place agent execution on a specific daemon,
//     ref = { local_path, daemon_id, label? }
export type ProjectResourceType = "github_repo" | "local_directory";

export interface GithubRepoResourceRef {
  url: string;
  default_branch_hint?: string;
}

export interface LocalDirectoryResourceRef {
  local_path: string;
  daemon_id: string;
  label?: string;
}

export type ProjectResourceRef =
  | GithubRepoResourceRef
  | LocalDirectoryResourceRef
  | Record<string, unknown>;

export interface ProjectResource {
  id: string;
  project_id: string;
  workspace_id: string;
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label: string | null;
  position: number;
  created_at: string;
  created_by: string | null;
}

export interface CreateProjectResourceRequest {
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label?: string;
  position?: number;
}

// resource_type is immutable server-side; partial-update payload mirrors that.
// Sending only the field(s) you want to change is fine — the server merges
// the request body with the existing row, including resource_ref shortcuts.
export interface UpdateProjectResourceRequest {
  resource_ref?: ProjectResourceRef;
  label?: string | null;
  position?: number;
}

export interface ListProjectResourcesResponse {
  resources: ProjectResource[];
  total: number;
}

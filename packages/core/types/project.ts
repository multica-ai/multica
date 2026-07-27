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
  // Calendar days ("YYYY-MM-DD"), no time-of-day or timezone — same contract as
  // issue.start_date / issue.due_date.
  start_date: string | null;
  due_date: string | null;
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
  start_date?: string;
  due_date?: string;
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
  // Omit the key to leave the date untouched; send null (or "") to clear it.
  start_date?: string | null;
  due_date?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

// ProjectResource is a typed pointer from a project to an external resource.
// The resource_ref shape depends on resource_type. New types add a case in
// validateAndNormalizeResourceRef on the server and a renderer in the UI.
//
// Known types (UI must default-case unknown server-side additions):
//   - github_repo: cloud-side git checkout, ref = { url, ref?, default_branch_hint? }
//   - local_directory: in-place agent execution on a specific daemon,
//     ref = { local_path, daemon_id, label? }
//   - project_space: server-managed project files, ref = { version: 1 }
export type ProjectResourceType = "github_repo" | "local_directory" | "project_space";

export interface GithubRepoResourceRef {
  url: string;
  ref?: string;
  default_branch_hint?: string;
}

export interface LocalDirectoryResourceRef {
  local_path: string;
  daemon_id: string;
  label?: string;
}

export interface ProjectSpaceResourceRef {
  version: 1;
}

export type ProjectResourceRef =
  | GithubRepoResourceRef
  | LocalDirectoryResourceRef
  | ProjectSpaceResourceRef
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

export interface ProjectSpaceEntry {
  name: string;
  relative_path: string;
  kind: "file" | "directory";
  size_bytes: number;
  modified_at: string;
}

export interface ListProjectSpaceFilesResponse {
  entries: ProjectSpaceEntry[];
  total: number;
}

export type ProjectSpaceImportStatus =
  | "queued"
  | "uploading"
  | "completed"
  | "partial"
  | "failed";

export interface ProjectSpaceImportFile {
  id: string;
  relative_path: string;
  stored_relative_path?: string;
  content_type: string;
  size_bytes: number;
  sha256?: string;
  status: "queued" | "uploading" | "completed" | "skipped" | "failed";
  error_code?: string;
}

export interface ProjectSpaceImport {
  id: string;
  project_id: string;
  batch_name: string;
  status: ProjectSpaceImportStatus;
  total_files: number;
  total_bytes: number;
  completed_files: number;
  failed_files: number;
  files: ProjectSpaceImportFile[];
}

export interface CreateProjectSpaceImportRequest {
  batch_name: string;
  files: Array<{
    relative_path: string;
    content_type: string;
    size_bytes: number;
  }>;
}

export interface ListProjectSpaceImportsResponse {
  imports: ProjectSpaceImport[];
  total: number;
}

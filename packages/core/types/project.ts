export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectPriority = "urgent" | "high" | "medium" | "low" | "none";

/**
 * Ship Hub pipeline topology. Determines which stages a release for this
 * project passes through:
 *   `staged`         → merging → in_staging → verifying → promoting → in_production → done
 *   `direct_to_prod` → merging → promoting → in_production → done
 * See migration 095 + completeMergeTrain in server/internal/service/ship/.
 */
export type ProjectPipelineKind = "staged" | "direct_to_prod";

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
  /**
   * Soft-delete marker. Non-null means the project is archived: it stays
   * in the DB and keeps its issue + resource references, but is hidden
   * from the default projects list. Restored by clearing back to null.
   */
  archived_at: string | null;
  archived_by: string | null;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
  resource_count: number;
  pipeline_kind: ProjectPipelineKind;
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
  pipeline_kind?: ProjectPipelineKind;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

// ProjectResource is a typed pointer from a project to an external resource.
// The resource_ref shape depends on resource_type (e.g. github_repo carries
// { url, default_branch_hint? }). New types add a case in
// validateAndNormalizeResourceRef on the server and a renderer in the UI;
// no schema or type changes required.
export type ProjectResourceType = "github_repo";

export interface GithubRepoResourceRef {
  url: string;
  default_branch_hint?: string;
}

export interface ProjectResource {
  id: string;
  project_id: string;
  workspace_id: string;
  resource_type: ProjectResourceType;
  resource_ref: GithubRepoResourceRef | Record<string, unknown>;
  label: string | null;
  position: number;
  created_at: string;
  created_by: string | null;
}

export interface CreateProjectResourceRequest {
  resource_type: ProjectResourceType;
  resource_ref: GithubRepoResourceRef | Record<string, unknown>;
  label?: string;
  position?: number;
}

export interface ListProjectResourcesResponse {
  resources: ProjectResource[];
  total: number;
}

import type { IssuePriority, IssueStatus } from "./issue";

export interface ManifestIssue {
  title: string;
  description?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
}

export interface WorkspaceExportManifest {
  schema_version: string;
  workspace: {
    id: string;
    slug: string;
    exported_at: string;
    source_app_version: string;
  };
  data: {
    issues: ManifestIssue[];
  };
}

export interface WorkspaceImportPayload {
  schema_version: string;
  source_type: string;
  workspace_id?: string;
  issues: ManifestIssue[];
}

export interface WorkspaceImportError {
  code: string;
  message: string;
}

export interface WorkspaceImportResult {
  summary: string;
  warnings?: string[];
  errors?: WorkspaceImportError[];
  created: number;
  skipped: number;
  failed: number;
}

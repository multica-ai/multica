export type AppScope = { resource_type: string; resource_id: string; access: "read" | "write" | "read_write" };
export type AppView = { id: string; type: "form" | "lookup" | "approval"; title: string; schema?: Record<string, unknown> };
export type AppManifest = { schema_version: "1"; name: string; version: string; scopes: AppScope[]; views?: AppView[]; frontend?: { entry: string }; backend?: { entry: string } };

export type WorkflowTriggerType = "schedule" | "webhook" | "data_event" | "manual" | "chat";
export type WorkflowStepType = "registry.read" | "registry.write" | "filter" | "view.show_and_wait";
export type WorkflowNode<Type extends string = string> = { id: string; type: Type; config: Record<string, unknown>; output_schema?: Record<string, unknown> };
export type AppWorkflowDefinition = { schema_version: "1"; trigger: WorkflowNode<WorkflowTriggerType>; steps: WorkflowNode<WorkflowStepType>[] };

export type CatalogApp = {
  id: string;
  slug: string;
  name: string;
  description: string;
  icon: string;
  folder: string;
  current_version?: string;
  status: "draft" | "published" | "disabled";
};

export type AppVersion = {
  version: string;
  release_notes: string;
  grant_status: "pending" | "approved" | "revoked" | "not_requested";
  scopes: AppScope[];
  created_at?: string;
};

export type AppWorkflow = {
  id: string;
  name: string;
  version: string;
  enabled: boolean;
  definition: AppWorkflowDefinition;
};

export type AppDetail = CatalogApp & { versions: AppVersion[]; workflows: AppWorkflow[] };

export type WorkflowRun = {
  id: string;
  status: "queued" | "running" | "waiting" | "succeeded" | "failed" | "cancelled";
  step_log?: Array<{ id: string; type?: string; status: string; output?: unknown }>;
  error?: string;
};

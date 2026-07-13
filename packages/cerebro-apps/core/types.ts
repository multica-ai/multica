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

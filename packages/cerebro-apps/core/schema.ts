import type { AppManifest, AppWorkflowDefinition, WorkflowStepType, WorkflowTriggerType } from "./types";

const semver = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$/;
const triggers = new Set<WorkflowTriggerType>(["schedule", "webhook", "data_event", "manual", "chat"]);
const steps = new Set<WorkflowStepType>(["registry.read", "registry.write", "filter", "view.show_and_wait"]);
const views = new Set(["form", "lookup", "approval"]);

export function validateWorkflowDefinition(value: unknown): string[] {
  const errors: string[] = [];
  if (!value || typeof value !== "object") return ["Workflow must be an object"];
  const workflow = value as Partial<AppWorkflowDefinition>;
  if (workflow.schema_version !== "1") errors.push("schema_version must be 1");
  if (!workflow.trigger || !triggers.has(workflow.trigger.type as WorkflowTriggerType)) errors.push("Trigger type is not supported");
  const ids = new Set<string>();
  if (workflow.trigger?.id) ids.add(workflow.trigger.id);
  else errors.push("Trigger id is required");
  if (!Array.isArray(workflow.steps)) return [...errors, "steps must be an array"];
  for (const step of workflow.steps) {
    if (!steps.has(step.type as WorkflowStepType)) errors.push(`Step type ${step.type} is not supported in v1`);
    if (!step.id) errors.push("Every step requires an id");
    else if (ids.has(step.id)) errors.push(`Duplicate node id ${step.id}`);
    else ids.add(step.id);
  }
  return errors;
}

export function validateAppManifest(value: unknown): string[] {
  const errors: string[] = [];
  if (!value || typeof value !== "object") return ["Manifest must be an object"];
  const manifest = value as Partial<AppManifest>;
  if (manifest.schema_version !== "1") errors.push("schema_version must be 1");
  if (!manifest.name?.trim()) errors.push("name is required");
  if (!manifest.version || !semver.test(manifest.version)) errors.push("version must use semantic versioning");
  if (!Array.isArray(manifest.scopes)) errors.push("scopes must be an array");
  for (const view of manifest.views ?? []) {
    if (!view.id || !view.title || !views.has(view.type)) errors.push("Each view requires an id, title, and supported type");
  }
  return errors;
}

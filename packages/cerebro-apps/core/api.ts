import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import type { AppAdminSummary, AppDetail, AppFolder, AppWorkflowDefinition, CatalogApp, WorkflowRun } from "./types";

const scopeSchema = z.object({ resource_type: z.string(), resource_id: z.string(), access: z.enum(["read", "write", "read_write"]) });
const nodeSchema = z.object({ id: z.string(), type: z.string(), config: z.record(z.string(), z.unknown()) });
const workflowDefinitionSchema = z.object({ schema_version: z.literal("1"), trigger: nodeSchema, steps: z.array(nodeSchema) });
const catalogAppSchema = z.object({
  id: z.string(), slug: z.string(), name: z.string(), description: z.string().catch(""), icon: z.string().catch("blocks"), folder: z.string().catch(""),
  current_version: z.string().nullish().transform((value) => value ?? undefined), status: z.enum(["draft", "published", "disabled"]),
});
const detailSchema = catalogAppSchema.extend({
  versions: z.array(z.object({ version: z.string(), release_notes: z.string(), grant_status: z.enum(["pending", "approved", "revoked", "not_requested"]), scopes: z.array(scopeSchema), created_at: z.string().optional() })).catch([]),
  workflows: z.array(z.object({ id: z.string(), name: z.string(), version: z.string(), enabled: z.boolean(), definition: workflowDefinitionSchema })).catch([]),
});
const runSchema = z.object({ id: z.string(), status: z.enum(["queued", "running", "waiting", "succeeded", "failed", "cancelled"]), step_log: z.array(z.object({ id: z.string(), type: z.string().optional(), status: z.string(), output: z.unknown().optional() })).optional(), error: z.string().optional() });

function workspacePath(path: string, workspaceSlug?: string): string {
  if (!workspaceSlug) return path;
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}workspace_slug=${encodeURIComponent(workspaceSlug)}`;
}

export async function listApps(workspaceSlug?: string): Promise<{ apps: CatalogApp[] }> {
  const endpoint = workspacePath("/api/cerebro/apps", workspaceSlug);
  const raw = await api.cerebroRequest<unknown>(endpoint);
  return parseWithFallback(raw, z.object({ apps: z.array(catalogAppSchema) }), { apps: [] }, { endpoint: "/api/cerebro/apps" });
}

export async function listAppAdminOverview(workspaceSlug?: string): Promise<AppAdminSummary[]> {
  const raw = await api.cerebroRequest<unknown>(workspacePath("/api/cerebro/apps/admin-overview", workspaceSlug));
  const parsed = z.object({ apps: z.array(z.object({
    id: z.string(), name: z.string(), owner: z.string(), version: z.string(), status: z.string(), approved_scopes: z.array(scopeSchema),
    spend_cents: z.number(), runs: z.number().int(), failed_runs: z.number().int(), health: z.enum(["healthy", "attention", "disabled"]), touched: z.array(z.string()),
  })) }).safeParse(raw);
  if (!parsed.success) throw new Error("Could not read app administration overview");
  return parsed.data.apps;
}

export async function installAllergenFormatter(workspaceSlug?: string): Promise<{ id: string }> {
  return api.cerebroRequest<{ id: string }>(workspacePath("/api/cerebro/apps/builtins/allergen-formatter/install", workspaceSlug), { method: "POST", body: "{}" });
}

export async function listAppFolders(workspaceSlug?: string): Promise<AppFolder[]> {
  const raw = await api.cerebroRequest<unknown>(workspacePath("/api/cerebro/app-folders", workspaceSlug));
  const parsed = z.object({ folders: z.array(z.object({ id: z.string(), parent_id: z.string().nullish(), name: z.string() })) }).safeParse(raw);
  return parsed.success ? parsed.data.folders : [];
}
export async function createAppFolder(name: string, parentId?: string, workspaceSlug?: string): Promise<void> {
  await api.cerebroRequest(workspacePath("/api/cerebro/app-folders", workspaceSlug), { method: "POST", body: JSON.stringify({ name, parent_id: parentId || null }) });
}
export async function updateAppFolder(id: string, name: string, parentId?: string, workspaceSlug?: string): Promise<void> {
  await api.cerebroRequest(workspacePath(`/api/cerebro/app-folders/${encodeURIComponent(id)}`, workspaceSlug), { method: "PATCH", body: JSON.stringify({ name, parent_id: parentId || null }) });
}
export async function deleteAppFolder(id: string, workspaceSlug?: string): Promise<void> {
  await api.cerebroRequest(workspacePath(`/api/cerebro/app-folders/${encodeURIComponent(id)}`, workspaceSlug), { method: "DELETE" });
}
export async function moveAppToFolder(folderId: string, appId: string, workspaceSlug?: string): Promise<void> {
  await api.cerebroRequest(workspacePath(`/api/cerebro/app-folders/${encodeURIComponent(folderId)}/apps/${encodeURIComponent(appId)}`, workspaceSlug), { method: "PUT", body: "{}" });
}

export async function createApp(input: { name: string; slug: string; description: string; folder: string }, workspaceSlug?: string): Promise<CatalogApp> {
  const raw = await api.cerebroRequest<unknown>(workspacePath("/api/cerebro/apps", workspaceSlug), { method: "POST", body: JSON.stringify(input) });
  const parsed = catalogAppSchema.safeParse(raw);
  if (!parsed.success) throw new Error("The app was created with an invalid response");
  return parsed.data;
}

export async function getAppDetail(id: string, workspaceSlug?: string): Promise<AppDetail> {
  const raw = await api.cerebroRequest<unknown>(workspacePath(`/api/cerebro/apps/${encodeURIComponent(id)}`, workspaceSlug));
  const parsed = detailSchema.safeParse(raw);
  if (!parsed.success) throw new Error("Could not read this app");
  return parsed.data as AppDetail;
}

export async function createWorkflow(input: { app_id: string; name: string; version: string; definition: AppWorkflowDefinition }, workspaceSlug?: string): Promise<{ id: string }> {
  const raw = await api.cerebroRequest<unknown>(workspacePath("/api/cerebro/app-workflows", workspaceSlug), { method: "POST", body: JSON.stringify(input) });
  const parsed = z.object({ id: z.string() }).safeParse(raw);
  if (!parsed.success) throw new Error("The workflow was created with an invalid response");
  return parsed.data;
}

export async function testWorkflow(id: string, triggerPayload: unknown, workspaceSlug?: string): Promise<WorkflowRun> {
  const raw = await api.cerebroRequest<unknown>(workspacePath(`/api/cerebro/app-workflows/${encodeURIComponent(id)}/test`, workspaceSlug), { method: "POST", body: JSON.stringify({ trigger_payload: triggerPayload }) });
  const parsed = runSchema.safeParse(raw);
  if (!parsed.success) throw new Error("The workflow test returned an invalid response");
  return parsed.data;
}

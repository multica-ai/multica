import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import type { AppDetail, AppWorkflowDefinition, CatalogApp, WorkflowRun } from "./types";

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

export async function listApps(): Promise<{ apps: CatalogApp[] }> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/apps");
  return parseWithFallback(raw, z.object({ apps: z.array(catalogAppSchema) }), { apps: [] }, { endpoint: "/api/cerebro/apps" });
}

export async function createApp(input: { name: string; slug: string; description: string; folder: string }): Promise<CatalogApp> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/apps", { method: "POST", body: JSON.stringify(input) });
  const parsed = catalogAppSchema.safeParse(raw);
  if (!parsed.success) throw new Error("The app was created with an invalid response");
  return parsed.data;
}

export async function getAppDetail(id: string): Promise<AppDetail> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/apps/${encodeURIComponent(id)}`);
  const parsed = detailSchema.safeParse(raw);
  if (!parsed.success) throw new Error("Could not read this app");
  return parsed.data as AppDetail;
}

export async function createWorkflow(input: { app_id: string; name: string; version: string; definition: AppWorkflowDefinition }): Promise<{ id: string }> {
  const raw = await api.cerebroRequest<unknown>("/api/cerebro/app-workflows", { method: "POST", body: JSON.stringify(input) });
  const parsed = z.object({ id: z.string() }).safeParse(raw);
  if (!parsed.success) throw new Error("The workflow was created with an invalid response");
  return parsed.data;
}

export async function testWorkflow(id: string, triggerPayload: unknown): Promise<WorkflowRun> {
  const raw = await api.cerebroRequest<unknown>(`/api/cerebro/app-workflows/${encodeURIComponent(id)}/test`, { method: "POST", body: JSON.stringify({ trigger_payload: triggerPayload }) });
  const parsed = runSchema.safeParse(raw);
  if (!parsed.success) throw new Error("The workflow test returned an invalid response");
  return parsed.data;
}

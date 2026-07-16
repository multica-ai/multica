// FIR-3212 — client for the per-run production prompt snapshot evidence.
// GET /api/agents/{id}/prompt-snapshots (refs, newest first) and
// GET /api/agents/{id}/prompt-snapshots/{taskId} (full layers).
//
// Per the API Response Compatibility rules (CLAUDE.md): parsed through
// lenient zod schemas with explicit fallbacks — never cast. Layers may
// arrive as a JSON array (current server) or as a base64 string (a server
// that still returns the raw JSONB []byte); both normalize to an array.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";

const PromptSnapshotRefSchema = z
  .object({
    task_id: z.string().default(""),
    issue_id: z.string().nullish(),
    agent_context_version: z.string().default(""),
    agent_context_version_id: z.string().nullish(),
    provider: z.string().default(""),
    model: z.string().default(""),
    runtime_version: z.string().default(""),
    system_prompt_mode: z.string().default(""),
    sha256_original: z.string().default(""),
    sha256_redacted: z.string().default(""),
    total_bytes: z.number().default(0),
    redacted: z.boolean().default(false),
    created_at: z.string().nullish(),
  })
  .loose();

export type PromptSnapshotRef = z.infer<typeof PromptSnapshotRefSchema>;

const PromptSnapshotLayerSchema = z
  .object({
    name: z.string().default(""),
    delivery: z.string().default(""),
    byte_size: z.number().default(0),
    sha256_original: z.string().default(""),
    sha256_redacted: z.string().default(""),
    content_redacted: z.string().default(""),
  })
  .loose();

export type PromptSnapshotLayer = z.infer<typeof PromptSnapshotLayerSchema>;

const PromptSnapshotListSchema = z
  .object({ snapshots: z.array(PromptSnapshotRefSchema).default([]) })
  .loose();

const PromptSnapshotSchema = PromptSnapshotRefSchema.extend({
  layers: z
    .union([z.array(PromptSnapshotLayerSchema), z.string()])
    .default([]),
});

// Intersection (not Omit): Omit over a passthrough zod type collapses the
// named keys into the index signature, losing every field type.
export type PromptSnapshot = PromptSnapshotRef & {
  layers: PromptSnapshotLayer[];
};

/** Base64-encoded JSONB from an older server → parsed layer array. */
function normalizeLayers(
  layers: PromptSnapshotLayer[] | string,
): PromptSnapshotLayer[] {
  if (Array.isArray(layers)) return layers;
  try {
    const decoded = JSON.parse(atob(layers)) as unknown;
    const parsed = z.array(PromptSnapshotLayerSchema).safeParse(decoded);
    return parsed.success ? parsed.data : [];
  } catch {
    return [];
  }
}

export async function listAgentPromptSnapshots(
  agentId: string,
): Promise<PromptSnapshotRef[]> {
  const path = `/api/agents/${agentId}/prompt-snapshots`;
  const raw = await api.cerebroRequest<unknown>(path);
  const parsed = parseWithFallback(
    raw,
    PromptSnapshotListSchema,
    { snapshots: [] },
    { endpoint: path },
  ) as z.infer<typeof PromptSnapshotListSchema>;
  return parsed.snapshots;
}

export async function getAgentPromptSnapshot(
  agentId: string,
  taskId: string,
): Promise<PromptSnapshot | null> {
  const path = `/api/agents/${agentId}/prompt-snapshots/${taskId}`;
  const raw = await api.cerebroRequest<unknown>(path);
  const parsed = parseWithFallback(raw, PromptSnapshotSchema, null, {
    endpoint: path,
  }) as z.infer<typeof PromptSnapshotSchema> | null;
  if (!parsed) return null;
  return { ...parsed, layers: normalizeLayers(parsed.layers) };
}

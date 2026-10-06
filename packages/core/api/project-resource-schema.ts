import { z } from "zod";

const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);

export const WorktreeReadinessSchema = z.object({
  status: z.enum(["checking", "ready", "blocked", "unavailable"]),
  reason_code: z.string().optional(),
  checked_at: z.iso.datetime({ offset: true }).optional(),
  expires_at: z.iso.datetime({ offset: true }).optional(),
  file_count: count.optional(),
  total_bytes: count.optional(),
  max_files: count.optional(),
  max_bytes: count.optional(),
  symlink_count: count.optional(),
  largest_paths: z.array(z.object({
    path: z.string(),
    file_count: count,
    total_bytes: count,
  })).nullable().transform((value) => value ?? undefined).optional(),
  message: z.string().optional(),
}).superRefine((value, ctx) => {
  if (value.status === "ready" && (
    !value.checked_at || !value.expires_at ||
    value.file_count === undefined || value.total_bytes === undefined ||
    value.max_files === undefined || value.max_bytes === undefined ||
    value.symlink_count === undefined
  )) ctx.addIssue({ code: "custom", message: "Ready requires a complete measurement" });
});

export const ListProjectResourcesResponseSchema = z.object({
  resources: z.array(z.object({
    id: z.string(),
    project_id: z.string(),
    workspace_id: z.string(),
    resource_type: z.string(),
    resource_ref: z.record(z.string(), z.unknown()),
    label: z.string().nullable().default(null),
    position: z.number(),
    created_at: z.string(),
    created_by: z.string().nullable().default(null),
    // A malformed additive diagnostic must not hide an otherwise valid resource.
    worktree_readiness: WorktreeReadinessSchema.catch({ status: "unavailable" }).optional(),
  }).loose()).default([]),
  total: z.number().default(0),
});

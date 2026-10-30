import { parseWithFallback } from "../api/schema";
import { z } from "zod";

const fileSchema = z
  .object({
    path: z.string().min(1).max(4096),
    status: z.enum(["A", "M", "D", "T"]),
    additions: z.number().int().nonnegative(),
    deletions: z.number().int().nonnegative(),
    base_ref: z.string().optional(),
    head_ref: z.string().optional(),
    diff: z.string().max(200 * 1024),
    binary: z.boolean(),
    truncated: z.boolean(),
  })
  .transform((f) => ({
    path: f.path,
    status: f.status,
    additions: f.additions,
    deletions: f.deletions,
    diff: f.diff,
    binary: f.binary,
    truncated: f.truncated,
    baseRef: f.base_ref,
    headRef: f.head_ref,
  }));
const snapshotSchema = z
  .object({
    version: z.literal(1),
    base_ref: z.string(),
    head_ref: z.string(),
    files: z.array(fileSchema).max(100),
    truncated: z.boolean(),
    working_tree_dirty: z.boolean(),
    reason: z.enum(["no_repository", "capture_failed"]).optional(),
  })
  .transform((s) => ({
    version: s.version,
    baseRef: s.base_ref,
    headRef: s.head_ref,
    files: s.files,
    truncated: s.truncated,
    workingTreeDirty: s.working_tree_dirty,
    reason: s.reason,
  }));
const responseSchema = z
  .object({
    available: z.boolean(),
    reason: z.enum(["disabled", "not_captured"]).optional(),
    runs: z.array(
      z.object({
        task_id: z.string(),
        status: z.string(),
        created_at: z.string(),
        branch_name: z.string().optional(),
        file_count: z.number().int().nonnegative(),
      }),
    ),
    source_task_id: z.string().optional(),
    snapshot: snapshotSchema.optional(),
  })
  .refine(
    (r) => !r.available || (!!r.source_task_id && !!r.snapshot),
    "Available code changes require a source and snapshot",
  )
  .transform((r) => ({
    available: r.available,
    reason: r.reason,
    runs: r.runs.map((run) => ({
      taskId: run.task_id,
      status: run.status,
      createdAt: run.created_at,
      branchName: run.branch_name,
      fileCount: run.file_count,
    })),
    sourceTaskId: r.source_task_id,
    snapshot: r.snapshot,
  }));
export type CodeChanges = z.infer<typeof responseSchema>;
export type CodeChangeFile = z.infer<typeof fileSchema>;
export function parseCodeChanges(raw: unknown): CodeChanges {
  const result = parseWithFallback<CodeChanges | null>(
    raw,
    responseSchema,
    null,
    { endpoint: "GET /api/issues/:id/code-changes", omitReceived: true },
  );
  if (!result) throw new Error("Malformed code changes response");
  return result;
}
export const codeChangeKeys = {
  issue: (wsId: string, issueId: string) =>
    ["code-changes", wsId, issueId] as const,
  detail: (wsId: string, issueId: string, taskId?: string) =>
    [...codeChangeKeys.issue(wsId, issueId), taskId ?? "latest"] as const,
};

export interface DiffLine {
  text: string;
  kind: "context" | "add" | "remove" | "header";
  oldLine?: number;
  newLine?: number;
}
export function parseDiffLines(diff: string): DiffLine[] {
  let oldLine = 0,
    newLine = 0,
    inHunk = false;
  return diff.split("\n").map((text) => {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text);
    if (hunk) {
      oldLine = Number(hunk[1]);
      newLine = Number(hunk[2]);
      inHunk = true;
      return { text, kind: "header" };
    }
    if (!inHunk || text.startsWith("\\")) return { text, kind: "header" };
    if (text.startsWith("+")) return { text, kind: "add", newLine: newLine++ };
    if (text.startsWith("-"))
      return { text, kind: "remove", oldLine: oldLine++ };
    if (text.startsWith(" "))
      return { text, kind: "context", oldLine: oldLine++, newLine: newLine++ };
    return { text, kind: "header" };
  });
}

const settingSchema = z
  .object({ workspace_id: z.string().min(1), enabled: z.boolean() })
  .transform((value) => ({
    workspaceId: value.workspace_id,
    enabled: value.enabled,
  }));
export function parseCodeChangesSetting(raw: unknown) {
  const result = parseWithFallback<z.infer<typeof settingSchema> | null>(
    raw,
    settingSchema,
    null,
    { endpoint: "PUT /api/workspaces/:id/code-changes" },
  );
  if (!result) throw new Error("Malformed code changes setting response");
  return result;
}

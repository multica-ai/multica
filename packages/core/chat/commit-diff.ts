import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { TaskMessagePayload } from "../types";

export const COMMIT_DIFF_TOOL = "multica_commit_diff";
const commit = z.string().regex(/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/);
const commitDiffSchema = z.object({
  repository: z.string().min(1),
  repository_id: z.string().min(1),
  base: commit.optional(),
  head: commit.optional(),
  kind: z.enum(["commits", "worktree", "draft"]).optional(),
  unavailable: z.boolean().default(false),
  repositories_omitted: z.boolean().default(false),
  worktree: z.boolean().default(false),
  changes: z.array(
    z.object({
      path: z.string().min(1),
      kind: z.string(),
      diff: z.string().optional(),
      binary: z.boolean().optional(),
      truncated: z.boolean().optional(),
      unavailable: z.boolean().optional(),
    }),
  ),
  truncated: z.boolean().default(false),
})
  .refine((value) => value.unavailable || (value.base !== undefined && value.head !== undefined))
  .transform(({ kind, worktree, ...value }) => ({
    ...value,
    kind: kind ?? (worktree ? "worktree" : "commits"),
  }));
export type CommitDiff = z.infer<typeof commitDiffSchema>;

export function getCommitDiffs(
  messages: readonly TaskMessagePayload[],
): CommitDiff[] {
  const comparisons = new Map<string, CommitDiff>();
  for (const message of [...messages].sort((a, b) => a.seq - b.seq)) {
    if (message.type !== "tool_use" || message.tool !== COMMIT_DIFF_TOOL)
      continue;
    // Diff bodies contain source code: malformed payloads must not be copied
    // into schema diagnostics. The catch also keeps older clients rendering.
    const diff = parseWithFallback<CommitDiff | null>(
      message.input,
      commitDiffSchema.nullable().catch(null),
      null,
      { endpoint: "task/commit-diff" },
    );
    if (diff)
      comparisons.set(JSON.stringify([diff.repository_id, diff.kind]), diff);
  }
  return [...comparisons.values()];
}

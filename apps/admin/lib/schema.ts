import type { ZodType } from "zod";

// Local copy of packages/core/api/schema.ts's parseWithFallback convention.
// Not imported from @multica/core directly: that package pulls in a much
// larger surface (workspace/issues/inbox modules) for a single helper, and
// apps/admin has no other reason to depend on it — see CLAUDE.md's
// "no speculative generality" persona rule. The behavior is identical:
// never throw into the UI on a drifted external contract, log and fall back.

export interface ParseOptions {
  /** Endpoint identifier used in the warning log so a drifted contract is
   *  greppable in production logs. */
  endpoint: string;
}

export function parseWithFallback<T>(
  data: unknown,
  schema: ZodType<T>,
  fallback: T,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  console.warn(`[admin] API response failed schema validation: ${opts.endpoint}`, {
    issues: result.error.issues,
  });
  return fallback;
}

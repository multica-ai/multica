/**
 * Mobile-owned parseWithFallback. Mirrors packages/core/api/schema.ts —
 * the boundary defense for installed-app schema drift required by root
 * CLAUDE.md "API Response Compatibility" and apps/mobile/CLAUDE.md
 * "Type drift defense".
 *
 * Why we mirror instead of import: keeps mobile fully decoupled and lets
 * us route the warning into mobile's own logger instead of the core
 * schemaLogger singleton. Behavior is identical: safeParse → on success
 * return parsed; on failure log + return fallback (never throw into UI).
 */
import { type ZodType } from "zod";

export interface ParseOptions {
  endpoint: string;
}

export function parseWithFallback<T>(
  data: unknown,
  schema: ZodType,
  fallback: T,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  console.warn(`[api] schema validation failed: ${opts.endpoint}`, {
    issues: result.error.issues,
    received: data,
  });
  return fallback;
}

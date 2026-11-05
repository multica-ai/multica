import { z } from "zod";
import type { Step } from "../contracts/resources.js";
import { canonicalJson, digest } from "../host/storage.js";
import { problem, VerificationError } from "../host/problem.js";
export const eventFilterSchema = z
  .object({
    sources: z
      .array(z.enum(["daemon", "main", "renderer", "backend", "page"]))
      .max(4)
      .optional(),
    types: z.array(z.string().min(1).max(160)).max(50).optional(),
    conversationId: z.string().optional(),
    turnId: z.string().optional(),
    requestId: z.string().optional(),
  })
  .strict();
const cursorSchema = z
  .object({
    version: z.literal(1),
    scope: z.string(),
    filter: z.string(),
    offset: z.number().int().nonnegative(),
  })
  .strict();
export function pageEvents(
  step: Step,
  filter: z.input<typeof eventFilterSchema> = {},
  cursor?: string,
  limit = 100,
) {
  filter = eventFilterSchema.parse(filter);
  z.number().int().min(1).max(200).parse(limit);
  const scope = digest(
    canonicalJson({
      stepId: step.stepId,
      environmentId: step.environmentId,
      subjectRef: step.subjectRef,
      dataGeneration: step.dataGeneration,
      generations: step.generations,
      startSnapshotId: step.startSnapshotId,
    }),
  );
  const fingerprint = digest(canonicalJson(filter));
  let offset = 0;
  if (cursor) {
    try {
      const decoded = cursorSchema.parse(
        JSON.parse(Buffer.from(cursor, "base64url").toString()),
      );
      if (decoded.scope !== scope || decoded.filter !== fingerprint)
        throw new Error("Cursor scope changed");
      offset = decoded.offset;
    } catch {
      throw new VerificationError(
        problem(
          "staleReference",
          "The event cursor belongs to another observation scope, generation or filter",
          "reobserve",
        ),
      );
    }
  }
  const items = step.events.filter(
    (item) =>
      (!filter.sources || filter.sources.includes(item.source as "daemon")) &&
      (!filter.types || filter.types.includes(item.type)) &&
      (!filter.conversationId ||
        filter.conversationId === item.conversationId) &&
      (!filter.turnId || filter.turnId === item.turnId) &&
      (!filter.requestId || filter.requestId === item.requestId),
  );
  if (offset > items.length)
    throw new VerificationError(
      problem(
        "staleReference",
        "The event cursor exceeds the retained observation",
        "reobserve",
      ),
    );
  const end = Math.min(items.length, offset + limit);
  return {
    items: items.slice(offset, end),
    nextCursor: Buffer.from(
      JSON.stringify({ version: 1, scope, filter: fingerprint, offset: end }),
    ).toString("base64url"),
    hasMore: end < items.length,
    coverage: step.coverage,
    gaps: step.gaps,
  };
}

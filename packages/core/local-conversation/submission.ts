import { z } from "zod";

export const SUBMISSION_LIFECYCLE_CAPABILITY = "submission-lifecycle-v1";
export const localSubmissionSchema = z.object({
  request_id: z.string().min(1),
  revision: z.number().int().positive(),
  state: z
    .enum(["queued", "starting", "attached", "removed", "recovery_required"])
    .catch("recovery_required"),
  item: z.record(z.string(), z.unknown()).default({}),
  position: z.number().int().nullable().default(null),
  blocked_reason: z.string().default(""),
  message_id: z.string().default(""),
  attempt: z
    .object({
      attempt_id: z.string().min(1),
      request_id: z.string().min(1),
      turn_ids: z.array(z.string()).default([]),
      delivery: z
        .enum(["not_sent", "intent_recorded", "confirmed", "unknown"])
        .catch("unknown"),
      settings: z.record(z.string(), z.unknown()).default({}),
    })
    .nullable()
    .default(null),
});
export type LocalSubmission = z.infer<typeof localSubmissionSchema>;

export const localSubmissionEventSchema = z.object({
  submission: localSubmissionSchema,
  queue_revision: z.number().int().nonnegative(),
});
export const localSubmissionStatusesSchema = z.object({
  store_generation: z.string(),
  snapshot_seq: z.number().int().nonnegative(),
  queue_revision: z.number().int().nonnegative(),
  submissions: z.array(localSubmissionSchema).default([]),
  known_history: z.boolean().default(false),
});

/** Partial replies never advance a conversation's full event cursor. */
export function mergeSubmission(
  previous: LocalSubmission | undefined,
  next: LocalSubmission,
): LocalSubmission {
  return previous && previous.revision >= next.revision ? previous : next;
}

import { z } from "zod";

// Unix seconds in the range supported by Date and Intl.DateTimeFormat.
const timestampSchema = z.number().int().positive().max(8_640_000_000_000);

const windowSchema = z.object({
  usedPercent: z.number().min(0).max(100),
  windowDurationMins: z.number().int().positive().optional(),
  resetsAt: timestampSchema.optional(),
});

export const subscriptionQuotaSnapshotSchema = z
  .object({
    observedAt: timestampSchema,
    exhausted: z.boolean().optional(),
    limitResetsAt: timestampSchema.optional(),
    primary: windowSchema.optional(),
    secondary: windowSchema.optional(),
  })
  .refine((snapshot) => !!(snapshot.primary || snapshot.secondary || snapshot.exhausted) &&
    (snapshot.limitResetsAt === undefined || snapshot.exhausted === true));

export const subscriptionQuotaReportSchema = z.object({
  status: z.enum(["idle", "pending", "complete", "unavailable", "unsupported", "failed", "timeout"]).catch("failed"),
  snapshot: subscriptionQuotaSnapshotSchema.optional().catch(undefined),
});

export type SubscriptionQuotaSnapshot = z.infer<typeof subscriptionQuotaSnapshotSchema>;
export type SubscriptionQuotaReport = z.infer<typeof subscriptionQuotaReportSchema>;

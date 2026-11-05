import { z } from "zod";

export const jsonSchema: z.ZodType<Json> = z.lazy(() =>
  z.union([
    z.null(),
    z.boolean(),
    z.number().finite(),
    z.string(),
    z.array(jsonSchema),
    z.record(z.string(), jsonSchema),
  ]),
);
export type Json =
  | null
  | boolean
  | number
  | string
  | Json[]
  | { [key: string]: Json };
export function toJson(value: unknown): Json {
  if (Array.isArray(value)) return value.map(toJson);
  if (
    value !== null &&
    typeof value === "object" &&
    Object.getPrototypeOf(value) === Object.prototype
  ) {
    const output: Record<string, Json> = {};
    for (const [key, child] of Object.entries(value))
      if (child !== undefined) output[key] = toJson(child);
    return output;
  }
  return jsonSchema.parse(value);
}

export const resourceIdSchema = z.uuid();
export const requestIdSchema = z
  .string()
  .min(1)
  .max(160)
  .regex(/^[\w.:-]+$/);
export const problemSchema = z
  .object({
    code: z.enum([
      "invalidInput",
      "forbidden",
      "notFound",
      "requestConflict",
      "dependencyMissing",
      "permissionRequired",
      "instanceBusy",
      "identityMismatch",
      "unsupportedVersion",
      "unsupportedCapability",
      "sourceUnavailable",
      "coverageIncomplete",
      "dataPartial",
      "staleReference",
      "deadlineExceeded",
      "outcomeUnknown",
      "interrupted",
      "cancelled",
      "cleanupIncomplete",
      "internalError",
    ]),
    message: z.string(),
    recovery: z.enum([
      "none",
      "inspect",
      "sameRequest",
      "reobserve",
      "userAction",
    ]),
    relatedRefs: z.array(z.string()),
  })
  .strict();
export type Problem = z.infer<typeof problemSchema>;

export const receiptSchema = z
  .object({
    schemaVersion: z.literal(1),
    receiptId: resourceIdSchema,
    requestId: requestIdSchema,
    operation: z.string().min(1),
    inputHash: z.string().regex(/^[a-f0-9]{64}$/),
    jobId: resourceIdSchema,
    createdAt: z.iso.datetime(),
  })
  .strict();
export type Receipt = z.infer<typeof receiptSchema>;

export const jobSchema = z
  .object({
    schemaVersion: z.literal(1),
    jobId: resourceIdSchema,
    receiptId: resourceIdSchema,
    operation: z.string().min(1),
    resourceRefs: z.array(resourceIdSchema),
    phase: z.string(),
    state: z.enum([
      "queued",
      "running",
      "awaitingUser",
      "cancelling",
      "completed",
      "failed",
      "cancelled",
      "interrupted",
      "unknown",
    ]),
    createdAt: z.iso.datetime(),
    updatedAt: z.iso.datetime(),
    output: z
      .object({ schema: z.string(), value: jsonSchema })
      .strict()
      .optional(),
    problem: problemSchema.optional(),
  })
  .strict()
  .superRefine((job, ctx) => {
    if (job.state === "completed" && !job.output)
      ctx.addIssue({
        code: "custom",
        message: "Completed jobs require output",
      });
    if (job.state !== "completed" && job.output)
      ctx.addIssue({
        code: "custom",
        message: "Only completed jobs have output",
      });
    if (
      ["failed", "cancelled", "interrupted", "unknown"].includes(job.state) &&
      !job.problem
    ) {
      ctx.addIssue({
        code: "custom",
        message: "Unsuccessful terminal jobs require a problem",
      });
    }
  });
export type Job = z.infer<typeof jobSchema>;

export const createVerificationInputSchema = z
  .object({
    title: z.string().trim().min(1).max(500),
    kind: z.enum(["smoke", "bug"]),
    requireScreenshots: z.boolean().default(false),
  })
  .strict();
export const verificationSchema = z
  .object({
    schemaVersion: z.literal(1),
    verificationId: resourceIdSchema,
    projectRef: z.string().regex(/^[a-f0-9]{64}$/),
    title: z.string().min(1).max(500),
    kind: z.enum(["smoke", "bug"]),
    state: z.enum(["open", "finalized", "pruned"]),
    pinned: z.boolean().default(false),
    createdAt: z.iso.datetime(),
    requireScreenshots: z.boolean().default(false),
    environments: z
      .array(
        z
          .object({
            environmentId: resourceIdSchema,
            role: z.enum(["baseline", "candidate", "smoke"]),
          })
          .strict(),
      )
      .default([]),
    revisions: z.array(resourceIdSchema).default([]),
  })
  .strict();
export type VerificationRecord = z.infer<typeof verificationSchema>;

export type ReadResult<T> = { schemaVersion: 1; observedAt: string } & (
  | { kind: "ok"; value: T }
  | { kind: "error"; problem: Problem }
);
export type CommandResult<T extends Json = Json> = {
  schemaVersion: 1;
  receiptId: string;
  requestId: string;
} & (
  | { kind: "accepted"; jobId: string }
  | { kind: "completed"; value: T }
  | { kind: "rejected" | "unknown"; problem: Problem }
);

export function readOk<T>(value: T): ReadResult<T> {
  return {
    schemaVersion: 1,
    observedAt: new Date().toISOString(),
    kind: "ok",
    value,
  };
}
export function readError<T = never>(problem: Problem): ReadResult<T> {
  return {
    schemaVersion: 1,
    observedAt: new Date().toISOString(),
    kind: "error",
    problem,
  };
}

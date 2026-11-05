import { z } from "zod";
import { jsonSchema, resourceIdSchema, problemSchema } from "./index.js";

export const scopeSchema = z
  .object({
    apiOrigin: z.url(),
    userId: z.string().min(1),
    workspaceId: z.string().min(1),
  })
  .strict();
export const bindingSchema = z
  .object({
    ref: z.string().min(1).max(160),
    kind: z.enum(["daemon", "backend", "desktop"]),
    baseUrl: z.url(),
    scope: scopeSchema,
    tokenFile: z.string().min(1),
    access: z.enum(["readOnly", "test"]),
    identity: z
      .object({
        ownerId: z.string().optional(),
        runId: z.string().optional(),
        commit: z.string().optional(),
        pid: z.number().int().positive().optional(),
      })
      .strict(),
    databaseFile: z.string().optional(),
    filesRoot: z.string().optional(),
  })
  .strict();
export type SourceBinding = z.infer<typeof bindingSchema>;
export const environmentSchema = z
  .object({
    schemaVersion: z.literal(1),
    environmentId: resourceIdSchema,
    projectRef: z.string(),
    commit: z.string(),
    checkout: z.string(),
    mode: z.enum(["renderer", "desktop"]),
    dataMode: z.enum(["liveReadOnly", "realSnapshot", "fixture"]),
    state: z.enum([
      "preparing",
      "prepared",
      "starting",
      "awaitingUser",
      "ready",
      "stopping",
      "stopped",
      "failed",
      "interrupted",
    ]),
    apiOrigin: z.url(),
    sourceRef: z.string().optional(),
    startedAt: z.string().optional(),
    components: z.record(z.string(), jsonSchema),
    problem: problemSchema.optional(),
  })
  .strict();
export type Environment = z.infer<typeof environmentSchema>;
export const sourceSchema = z
  .object({
    schemaVersion: z.literal(1),
    sourceId: resourceIdSchema,
    bindingRef: z.string(),
    kind: z.enum(["daemon", "backend", "desktop"]),
    scope: scopeSchema,
  })
  .strict();
export const evidenceSchema = z
  .object({
    schemaVersion: z.literal(1),
    evidenceId: resourceIdSchema,
    verificationId: resourceIdSchema,
    environmentId: resourceIdSchema.optional(),
    stepId: resourceIdSchema.optional(),
    kind: z.enum([
      "screenshot",
      "snapshot",
      "events",
      "check",
      "log",
      "data",
      "report",
    ]),
    source: z.string(),
    path: z.string(),
    sha256: z.string().regex(/^[a-f0-9]{64}$/),
    bytes: z.number().int().nonnegative(),
    mediaType: z.string(),
    capturedAt: z.string(),
    receivedAt: z.string(),
    coverage: z.enum(["complete", "partial", "unknown"]),
  })
  .strict();
export type Evidence = z.infer<typeof evidenceSchema>;
export const observationSchema = z
  .object({
    source: z.enum(["daemon", "main", "renderer", "backend"]),
    status: z.enum(["available", "partial", "unavailable"]),
    startedAt: z.string(),
    endedAt: z.string(),
    instance: z.string(),
    generation: z.string().optional(),
    cursor: z.number().int().nonnegative().optional(),
    payload: jsonSchema.optional(),
    problem: problemSchema.optional(),
  })
  .strict();
export const stateSnapshotSchema = z
  .object({
    schemaVersion: z.literal(1),
    stateSnapshotId: resourceIdSchema,
    environmentId: resourceIdSchema,
    subjectRef: z.string(),
    consistency: z.literal("perSource"),
    observations: z.array(observationSchema),
  })
  .strict();
export type StateSnapshot = z.infer<typeof stateSnapshotSchema>;
export const eventSchema = z
  .object({
    eventId: z.string(),
    source: z.string(),
    instance: z.string(),
    generation: z.string(),
    cursor: z.number().int().nonnegative(),
    type: z.string(),
    occurredAt: z.string().optional(),
    receivedAt: z.string(),
    conversationId: z.string().optional(),
    turnId: z.string().optional(),
    requestId: z.string().optional(),
    payload: jsonSchema,
  })
  .strict();
export type ObservedEvent = z.infer<typeof eventSchema>;
export const stepSchema = z
  .object({
    schemaVersion: z.literal(1),
    stepId: resourceIdSchema,
    verificationId: resourceIdSchema,
    environmentId: resourceIdSchema,
    subjectRef: z.string(),
    description: z.string(),
    driver: z.enum(["playwright", "computerUse", "api"]),
    state: z.enum(["observing", "ended", "interrupted"]),
    startedAt: z.string(),
    endedAt: z.string().optional(),
    startSnapshotId: resourceIdSchema,
    endSnapshotId: resourceIdSchema.optional(),
    startCursors: z.record(z.string(), z.number().int().nonnegative()),
    endCursors: z.record(z.string(), z.number().int().nonnegative()),
    generations: z.record(z.string(), z.string()),
    coverage: z.enum(["complete", "partial", "unknown"]),
    gaps: z.array(z.string()),
    events: z.array(eventSchema),
  })
  .strict();
export type Step = z.infer<typeof stepSchema>;
export const temporalSchema = z.discriminatedUnion("mode", [
  z.object({ mode: z.literal("now") }).strict(),
  z
    .object({
      mode: z.literal("eventually"),
      timeoutMs: z.number().int().min(1).max(120000),
    })
    .strict(),
  z
    .object({ mode: z.literal("throughout"), stepId: resourceIdSchema })
    .strict(),
]);
export const pageTargetSchema = z
  .object({
    windowRef: resourceIdSchema,
    frameRef: resourceIdSchema.optional(),
    locator: z.discriminatedUnion("kind", [
      z
        .object({
          kind: z.literal("testId"),
          value: z.string().min(1).max(200),
        })
        .strict(),
      z
        .object({ kind: z.literal("text"), value: z.string().min(1).max(200) })
        .strict(),
      z
        .object({ kind: z.literal("label"), value: z.string().min(1).max(200) })
        .strict(),
    ]),
  })
  .strict();
export const predicateSchema = z
  .object({
    kind: z.enum([
      "queue.itemCount",
      "queue.paused",
      "turn.status",
      "providerFixture.receivedCount",
      "diagnostic.errorCount",
      "page.visible",
      "page.text",
      "page.controlState",
    ]),
    operator: z.enum(["eq", "contains", "gte", "lte"]),
    expected: jsonSchema,
    target: z.union([z.string(), pageTargetSchema]).optional(),
  })
  .strict();
export const checkSchema = z
  .object({
    schemaVersion: z.literal(1),
    checkId: resourceIdSchema,
    comparisonKey: z.string().min(1).max(160).optional(),
    verificationId: resourceIdSchema,
    environmentId: resourceIdSchema,
    stepId: resourceIdSchema.optional(),
    subjectRef: z.string(),
    predicate: predicateSchema,
    temporal: temporalSchema,
    status: z.enum(["passed", "failed", "unavailable"]),
    actual: jsonSchema.optional(),
    startedAt: z.string(),
    endedAt: z.string(),
    evidenceIds: z.array(resourceIdSchema),
    problem: problemSchema.optional(),
  })
  .strict();
export type Check = z.infer<typeof checkSchema>;
export const dataSnapshotSchema = z
  .object({
    schemaVersion: z.literal(1),
    dataSnapshotId: resourceIdSchema,
    sourceId: resourceIdSchema,
    format: z.enum(["multica.local-conversation/v1", "multica.workspace/v1"]),
    capturedAt: z.string(),
    consistency: z.enum(["perConversation", "perEntity"]),
    content: jsonSchema,
    sha256: z.string(),
    complete: z.boolean(),
  })
  .strict();
export type DataSnapshot = z.infer<typeof dataSnapshotSchema>;
export const datasetSchema = z
  .object({
    schemaVersion: z.literal(1),
    datasetId: resourceIdSchema,
    environmentId: resourceIdSchema,
    mode: z.enum(["liveReadOnly", "realSnapshot", "fixture"]),
    sourceId: resourceIdSchema.optional(),
    dataSnapshotId: resourceIdSchema.optional(),
    restoreVersion: z.number().int().positive(),
    transformations: z.array(z.string()).default([]),
    refs: z.record(z.string(), z.string()),
    state: z.enum(["ready", "partial"]),
    resources: z.array(z.string()),
  })
  .strict();
export type Dataset = z.infer<typeof datasetSchema>;

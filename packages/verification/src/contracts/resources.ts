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
    verificationTokenFile: z.string().optional(),
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
    attachmentOrigins: z.array(z.url()).max(10).optional(),
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
export const dataSnapshotSchema = z
  .object({
    schemaVersion: z.literal(1),
    dataSnapshotId: resourceIdSchema,
    sourceRef: z.string(),
    format: z.enum(["multica.local-conversation/v1", "multica.workspace/v1"]),
    capturedAt: z.string(),
    captureStartedAt: z.iso.datetime(),
    sourceVersion: z.string().nullable(),
    sourceScope: scopeSchema,
    capabilities: z.array(z.string()),
    entities: z.array(
      z.object({ type: z.string(), originalId: z.string() }).strict(),
    ),
    relationships: z.array(
      z.object({ from: z.string(), to: z.string(), kind: z.string() }).strict(),
    ),
    attachments: z.array(
      z
        .object({
          originalId: z.string(),
          relativePath: z.string(),
          bytes: z.number().int().nonnegative(),
          sha256: z.string(),
        })
        .strict(),
    ),
    missing: z.array(z.string()),
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
    mode: z.enum(["realSnapshot", "fixture"]),
    dataSnapshotId: resourceIdSchema,
    transformations: z.array(z.string()).default([]),
    refs: z.record(z.string(), z.string()),
    resources: z.array(z.string()),
    providerScript: z.literal("controlled/v1").optional(),
  })
  .strict();
export type Dataset = z.infer<typeof datasetSchema>;

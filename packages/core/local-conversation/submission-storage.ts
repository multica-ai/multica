import { z } from "zod";
import { localSubmissionSchema } from "./submission";

const block = z.object({
  type: z.enum(["text", "image", "file", "skill", "mention", "connector"]),
  text: z.string().optional(),
  mimeType: z.string().optional(),
  data: z.string().optional(),
  url: z.string().optional(),
  attachmentId: z.string().optional(),
  assetId: z.string().optional(),
  name: z.string().optional(),
  path: z.string().optional(),
  connectorKey: z.string().optional(),
  uri: z.string().optional(),
  hostPath: z.string().optional(),
  uploadStatus: z.string().optional(),
  kind: z.string().optional(),
  sizeBytes: z.number().nonnegative().optional(),
});
export const persistedQueuedPromptSchema = z.object({
  requestId: z.string().min(1),
  content: z.string(),
  prompt: z.string().optional(),
  attachmentIds: z.array(z.string()).optional(),
  references: z
    .array(
      z.object({ kind: z.enum(["session", "issue", "agent"]), id: z.string() }),
    )
    .optional(),
  agents: z.array(z.object({ id: z.string(), name: z.string() })).optional(),
  model: z.string().nullable().optional(),
  reasoningEffort: z
    .enum(["none", "minimal", "low", "medium", "high", "xhigh", "max"])
    .nullable()
    .optional(),
  permissionMode: z.enum(["default", "auto", "full-access"]).optional(),
  display: z.object({
    id: z.string(),
    clientSubmitId: z.string().optional(),
    createdAtUnixMs: z.number(),
    content: z.array(block),
    runtimeContent: z.array(block).optional(),
    displayPrompt: z.string().optional(),
    requiredSettingsPatch: z
      .object({
        browserUse: z.boolean().optional(),
        computerUse: z.boolean().optional(),
      })
      .optional(),
  }),
});

export const storedSubmissionSchema = z.object({
  version: z.literal(1),
  requestId: z.string().min(1),
  sessionId: z.string().min(1),
  sequence: z.number().int().nonnegative(),
  createdAt: z.number(),
  kind: z.enum(["prompt", "activation"]),
  stage: z.enum(["unconfirmed", "confirmed"]),
  generation: z.string().default(""),
  lifecycle: z.boolean().default(false),
  input: z.record(z.string(), z.unknown()),
  prepared: persistedQueuedPromptSchema.optional(),
  authority: localSubmissionSchema.optional(),
  queue: z
    .object({
      revision: z.number().int().nonnegative(),
      paused: z.boolean(),
      error: z.string().optional(),
      position: z.number().int().nonnegative(),
    })
    .optional(),
});
export type StoredSubmission = z.infer<typeof storedSubmissionSchema>;

/** Bound to API origin, user, workspace and the stable execution device. Writes reject on failure. */
export interface SubmissionStorage {
  read(): Promise<StoredSubmission[]>;
  put(record: StoredSubmission): Promise<StoredSubmission>;
  confirm(
    requestId: string,
    authority?: z.infer<typeof localSubmissionSchema>,
  ): Promise<void>;
  remove(requestId: string): Promise<void>;
}
